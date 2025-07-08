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
	"github.com/docker/cli/cli/command"
	controlapi "github.com/moby/buildkit/api/services/control"
	"github.com/moby/buildkit/util/gitutil"
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
		eg.Go(func() error {
			if node.Driver == nil {
				return nil
			}
			var records []historyRecord
			c, err := node.Driver.Client(ctx)
			if err != nil {
				return err
			}

			var matchers []matchFunc
			if len(filters) > 0 {
				filters, matchers, err = dockerFiltersToBuildkit(filters)
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
		loop0:
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

				// for older buildkit that don't support filters apply local filters
				for _, matcher := range matchers {
					if !matcher(he.Record) {
						continue loop0
					}
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

func finalizeRecord(ctx context.Context, ref string, nodes []builder.Node) error {
	eg, ctx := errgroup.WithContext(ctx)
	for _, node := range nodes {
		eg.Go(func() error {
			if node.Driver == nil {
				return nil
			}
			c, err := node.Driver.Client(ctx)
			if err != nil {
				return err
			}
			_, err = c.ControlClient().UpdateBuildHistory(ctx, &controlapi.UpdateBuildHistoryRequest{
				Ref:      ref,
				Finalize: true,
			})
			return err
		})
	}
	return eg.Wait()
}

func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	return fmt.Sprintf("%dm %2ds", int(d.Minutes()), int(d.Seconds())%60)
}

type matchFunc func(*controlapi.BuildHistoryRecord) bool

func dockerFiltersToBuildkit(in []string) ([]string, []matchFunc, error) {
	out := []string{}
	matchers := []matchFunc{}
	for _, f := range in {
		key, value, sep, found := cutAny(f, "!=", "=", "<=", "<", ">=", ">")
		if !found {
			return nil, nil, errors.Errorf("invalid filter %q", f)
		}
		switch key {
		case "ref", "repository", "status":
			if sep != "=" && sep != "!=" {
				return nil, nil, errors.Errorf("invalid separator for %q, expected = or !=", f)
			}
			matchers = append(matchers, valueFiler(key, value, sep))
			if sep == "=" {
				if key == "status" {
					sep = "=="
				} else {
					sep = "~="
				}
			}
		case "startedAt", "completedAt", "duration":
			if sep == "=" || sep == "!=" {
				return nil, nil, errors.Errorf("invalid separator for %q, expected <=, <, >= or >", f)
			}
			matcher, err := timeBasedFilter(key, value, sep)
			if err != nil {
				return nil, nil, err
			}
			matchers = append(matchers, matcher)
		default:
			return nil, nil, errors.Errorf("unsupported filter %q", f)
		}
		out = append(out, key+sep+value)
	}
	return out, matchers, nil
}

func valueFiler(key, value, sep string) matchFunc {
	return func(rec *controlapi.BuildHistoryRecord) bool {
		var recValue string
		switch key {
		case "ref":
			recValue = rec.Ref
		case "repository":
			v, ok := rec.FrontendAttrs["vcs:source"]
			if ok {
				recValue = v
			} else {
				if context, ok := rec.FrontendAttrs["context"]; ok {
					if ref, err := gitutil.ParseGitRef(context); err == nil {
						recValue = ref.Remote
					}
				}
			}
		case "status":
			if rec.CompletedAt != nil {
				if rec.Error != nil {
					if strings.Contains(rec.Error.Message, "context canceled") {
						recValue = "canceled"
					} else {
						recValue = "error"
					}
				} else {
					recValue = "completed"
				}
			} else {
				recValue = "running"
			}
		}
		switch sep {
		case "=":
			if key == "status" {
				return recValue == value
			}
			return strings.Contains(recValue, value)
		case "!=":
			return recValue != value
		default:
			return false
		}
	}
}

func timeBasedFilter(key, value, sep string) (matchFunc, error) {
	var cmp int64
	switch key {
	case "startedAt", "completedAt":
		v, err := time.ParseDuration(value)
		if err == nil {
			tm := time.Now().Add(-v)
			cmp = tm.Unix()
		} else {
			tm, err := time.Parse(time.RFC3339, value)
			if err != nil {
				return nil, errors.Errorf("invalid time %s", value)
			}
			cmp = tm.Unix()
		}
	case "duration":
		v, err := time.ParseDuration(value)
		if err != nil {
			return nil, errors.Errorf("invalid duration %s", value)
		}
		cmp = int64(v)
	default:
		return nil, nil
	}

	return func(rec *controlapi.BuildHistoryRecord) bool {
		var val int64
		switch key {
		case "startedAt":
			val = rec.CreatedAt.AsTime().Unix()
		case "completedAt":
			if rec.CompletedAt != nil {
				val = rec.CompletedAt.AsTime().Unix()
			}
		case "duration":
			if rec.CompletedAt != nil {
				val = int64(rec.CompletedAt.AsTime().Sub(rec.CreatedAt.AsTime()))
			}
		}
		switch sep {
		case ">=":
			return val >= cmp
		case "<=":
			return val <= cmp
		case ">":
			return val > cmp
		default:
			return val < cmp
		}
	}, nil
}

func cutAny(s string, seps ...string) (before, after, sep string, found bool) {
	for _, sep := range seps {
		if idx := strings.Index(s, sep); idx != -1 {
			return s[:idx], s[idx+len(sep):], sep, true
		}
	}
	return s, "", "", false
}

func loadNodes(ctx context.Context, dockerCli command.Cli, builderName string) ([]builder.Node, error) {
	b, err := builder.New(dockerCli, builder.WithName(builderName))
	if err != nil {
		return nil, err
	}
	nodes, err := b.LoadNodes(ctx, builder.WithData())
	if err != nil {
		return nil, err
	}
	if ok, err := b.Boot(ctx); err != nil {
		return nil, err
	} else if ok {
		nodes, err = b.LoadNodes(ctx, builder.WithData())
		if err != nil {
			return nil, err
		}
	}
	for _, node := range nodes {
		if node.Err != nil {
			return nil, node.Err
		}
	}
	return nodes, nil
}
