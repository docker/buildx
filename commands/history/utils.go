package history

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
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

func queryRecords(ctx context.Context, ref string, nodes []builder.Node) ([]historyRecord, error) {
	var mu sync.Mutex
	var out []historyRecord

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
			serv, err := c.ControlClient().ListenBuildHistory(ctx, &controlapi.BuildHistoryRequest{
				EarlyExit: true,
				Ref:       ref,
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
	return out, nil
}

func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	return fmt.Sprintf("%dm %2ds", int(d.Minutes()), int(d.Seconds())%60)
}
