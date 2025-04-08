package history

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/docker/buildx/build"
	"github.com/docker/buildx/builder"
	"github.com/docker/buildx/localstate"
	controlapi "github.com/moby/buildkit/api/services/control"
	"github.com/pkg/errors"
	"golang.org/x/sync/errgroup"
)

const recordsLimit = 50

func buildName(fattrs map[string]string, ls *localstate.State) string {
	var res string

	var target, contextPath, dockerfilePath, vcsSource string
	if v, ok := fattrs["target"]; ok {
		target = v
	}
	if v, ok := fattrs["context"]; ok {
		contextPath = filepath.ToSlash(v)
	} else if v, ok := fattrs["vcs:localdir:context"]; ok && v != "." {
		contextPath = filepath.ToSlash(v)
	}
	if v, ok := fattrs["vcs:source"]; ok {
		vcsSource = v
	}
	if v, ok := fattrs["filename"]; ok && v != "Dockerfile" {
		dockerfilePath = filepath.ToSlash(v)
	}
	if v, ok := fattrs["vcs:localdir:dockerfile"]; ok && v != "." {
		dockerfilePath = filepath.ToSlash(filepath.Join(v, dockerfilePath))
	}

	var localPath string
	if ls != nil && !build.IsRemoteURL(ls.LocalPath) {
		if ls.LocalPath != "" && ls.LocalPath != "-" {
			localPath = filepath.ToSlash(ls.LocalPath)
		}
		if ls.DockerfilePath != "" && ls.DockerfilePath != "-" && ls.DockerfilePath != "Dockerfile" {
			dockerfilePath = filepath.ToSlash(ls.DockerfilePath)
		}
	}

	// remove default dockerfile name
	const defaultFilename = "/Dockerfile"
	hasDefaultFileName := strings.HasSuffix(dockerfilePath, defaultFilename) || dockerfilePath == ""
	dockerfilePath = strings.TrimSuffix(dockerfilePath, defaultFilename)

	// dockerfile is a subpath of context
	if strings.HasPrefix(dockerfilePath, localPath) && len(dockerfilePath) > len(localPath) {
		res = dockerfilePath[strings.LastIndex(localPath, "/")+1:]
	} else {
		// Otherwise, use basename
		bpath := localPath
		if len(dockerfilePath) > 0 {
			bpath = dockerfilePath
		}
		if len(bpath) > 0 {
			lidx := strings.LastIndex(bpath, "/")
			res = bpath[lidx+1:]
			if !hasDefaultFileName {
				if lidx != -1 {
					res = filepath.ToSlash(filepath.Join(filepath.Base(bpath[:lidx]), res))
				} else {
					res = filepath.ToSlash(filepath.Join(filepath.Base(bpath), res))
				}
			}
		}
	}

	if len(contextPath) > 0 {
		res = contextPath
	}
	if len(target) > 0 {
		if len(res) > 0 {
			res = res + " (" + target + ")"
		} else {
			res = target
		}
	}
	if res == "" && vcsSource != "" {
		return vcsSource
	}
	return res
}

func trimBeginning(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return ".." + s[len(s)-n+2:]
}

type historyRecord struct {
	*controlapi.BuildHistoryRecord
	currentTimestamp *time.Time
	node             *builder.Node
	name             string
}

type queryOptions struct {
	CompletedOnly bool
	Filters       []string
}

func queryRecords(ctx context.Context, ref string, nodes []builder.Node, opts *queryOptions) ([]historyRecord, error) {
	var mu sync.Mutex
	var out []historyRecord

	var offset *int
	if strings.HasPrefix(ref, "^") {
		off, err := strconv.Atoi(ref[1:])
		if err != nil {
			return nil, errors.Wrapf(err, "invalid offset %q", ref)
		}
		offset = &off
		ref = ""
	}

	var filters []string
	if opts != nil {
		filters = opts.Filters
	}

	eg, ctx := errgroup.WithContext(ctx)
	for _, node := range nodes {
		node := node
		eg.Go(func() error {
			if node.Driver == nil {
				return nil
			}
			var records []historyRecord
			c, err := node.Driver.Client(ctx)
			if err != nil {
				return err
			}

			if len(filters) > 0 {
				filters, err = dockerFiltersToBuildkit(filters)
				if err != nil {
					return err
				}
				sb := bytes.NewBuffer(nil)
				w := csv.NewWriter(sb)
				w.Write(filters)
				w.Flush()
				filters = []string{strings.TrimSuffix(sb.String(), "\n")}
			}

			serv, err := c.ControlClient().ListenBuildHistory(ctx, &controlapi.BuildHistoryRequest{
				EarlyExit: true,
				Ref:       ref,
				Limit:     recordsLimit,
				Filter:    filters,
			})
			if err != nil {
				return err
			}
			md, err := serv.Header()
			if err != nil {
				return err
			}
			var ts *time.Time
			if v, ok := md[headerKeyTimestamp]; ok {
				t, err := time.Parse(time.RFC3339Nano, v[0])
				if err != nil {
					return err
				}
				ts = &t
			}
			defer serv.CloseSend()
			for {
				he, err := serv.Recv()
				if err != nil {
					if errors.Is(err, io.EOF) {
						break
					}
					return err
				}
				if he.Type == controlapi.BuildHistoryEventType_DELETED || he.Record == nil {
					continue
				}
				if opts != nil && opts.CompletedOnly && he.Type != controlapi.BuildHistoryEventType_COMPLETE {
					continue
				}

				records = append(records, historyRecord{
					BuildHistoryRecord: he.Record,
					currentTimestamp:   ts,
					node:               &node,
				})
			}
			mu.Lock()
			out = append(out, records...)
			mu.Unlock()
			return nil
		})
	}

	if err := eg.Wait(); err != nil {
		return nil, err
	}

	slices.SortFunc(out, func(a, b historyRecord) int {
		return b.CreatedAt.AsTime().Compare(a.CreatedAt.AsTime())
	})

	if offset != nil {
		var filtered []historyRecord
		for _, r := range out {
			if *offset > 0 {
				*offset--
				continue
			}
			filtered = append(filtered, r)
			break
		}
		if *offset > 0 {
			return nil, errors.Errorf("no completed build found with offset %d", *offset)
		}
		out = filtered
	}

	return out, nil
}

func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	return fmt.Sprintf("%dm %2ds", int(d.Minutes()), int(d.Seconds())%60)
}

func dockerFiltersToBuildkit(in []string) ([]string, error) {
	out := []string{}
	for _, f := range in {
		key, value, sep, found := multiCut(f, "=", "!=", "<=", "<", ">=", ">")
		if !found {
			return nil, errors.Errorf("invalid filter %q", f)
		}
		switch key {
		case "ref", "repository", "status":
			if sep != "=" && sep != "!=" {
				return nil, errors.Errorf("invalid separator for %q, expected = or !=", f)
			}
			if sep == "=" {
				if key == "status" {
					sep = "=="
				} else {
					sep = "~="
				}
			}
		case "createdAt", "completedAt", "duration":
			if sep == "=" || sep == "!=" {
				return nil, errors.Errorf("invalid separator for %q, expected <=, <, >= or >", f)
			}
		}
		out = append(out, key+sep+value)
	}
	return out, nil
}

func multiCut(s string, seps ...string) (before, after, sep string, found bool) {
	for _, sep := range seps {
		if idx := strings.Index(s, sep); idx != -1 {
			return s[:idx], s[idx+len(sep):], sep, true
		}
	}
	return s, "", "", false
}
