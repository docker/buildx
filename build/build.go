package build

import (
	"context"
	"io"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/containerd/containerd/platforms"
	"github.com/moby/buildkit/client"
	"github.com/moby/buildkit/session"
	specs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/pkg/errors"
	"github.com/tonistiigi/buildx/driver"
	"github.com/tonistiigi/buildx/util/progress"
	"golang.org/x/sync/errgroup"
)

type Options struct {
	Inputs    Inputs
	Tags      []string
	Labels    map[string]string
	BuildArgs map[string]string
	Pull      bool

	NoCache   bool
	Target    string
	Platforms []specs.Platform
	Exports   []client.ExportEntry
	Session   []session.Attachable

	// DockerTarget
}

type Inputs struct {
	ContextPath    string
	DockerfilePath string
	InStream       io.Reader
}

func Build(ctx context.Context, drivers []driver.Driver, opt map[string]Options, pw progress.Writer) (map[string]*client.SolveResponse, error) {
	if len(drivers) == 0 {
		return nil, errors.Errorf("driver required for build")
	}

	if len(drivers) > 1 {
		return nil, errors.Errorf("multiple drivers currently not supported")
	}

	pwOld := pw
	d := drivers[0]
	_, isDefaultMobyDriver := d.(interface {
		IsDefaultMobyDriver()
	})
	c, pw, err := driver.Boot(ctx, d, pw)
	if err != nil {
		close(pwOld.Status())
		<-pwOld.Done()
		return nil, err
	}

	withPrefix := len(opt) > 1

	mw := progress.NewMultiWriter(pw)

	eg, ctx := errgroup.WithContext(ctx)

	resp := map[string]*client.SolveResponse{}
	var mu sync.Mutex

	for k, opt := range opt {
		pw := mw.WithPrefix(k, withPrefix)

		so := client.SolveOpt{
			Frontend:      "dockerfile.v0",
			FrontendAttrs: map[string]string{},
		}

		switch len(opt.Exports) {
		case 1:
			// valid
		case 0:
			if isDefaultMobyDriver {
				// backwards compat for docker driver only:
				// this ensures the build results in a docker image.
				opt.Exports = []client.ExportEntry{{Type: "image", Attrs: map[string]string{}}}
			}
		default:
			return nil, errors.Errorf("multiple outputs currently unsupported")
		}

		if len(opt.Tags) > 0 {
			for i, e := range opt.Exports {
				switch e.Type {
				case "image", "oci", "docker":
					opt.Exports[i].Attrs["name"] = strings.Join(opt.Tags, ",")
				}
			}
		} else {
			for _, e := range opt.Exports {
				if e.Type == "image" && e.Attrs["name"] == "" && e.Attrs["push"] != "" {
					if ok, _ := strconv.ParseBool(e.Attrs["push"]); ok {
						return nil, errors.Errorf("tag is needed when pushing to registry")
					}
				}
			}
		}

		for i, e := range opt.Exports {
			if e.Type == "oci" && !d.Features()[driver.OCIExporter] {
				return nil, notSupported(d, driver.OCIExporter)
			}
			if e.Type == "docker" {
				if e.Output == nil {
					if !isDefaultMobyDriver {
						return nil, errors.Errorf("loading to docker currently not implemented, specify dest file or -")
					}
					e.Type = "image"
				} else if !d.Features()[driver.DockerExporter] {
					return nil, notSupported(d, driver.DockerExporter)
				}
			}
			if e.Type == "image" && isDefaultMobyDriver {
				opt.Exports[i].Type = "moby"
				if e.Attrs["push"] != "" {
					if ok, _ := strconv.ParseBool(e.Attrs["push"]); ok {
						return nil, errors.Errorf("auto-push is currently not implemented for moby driver")
					}
				}
			}
		}

		// TODO: handle loading to docker daemon

		so.Exports = opt.Exports
		so.Session = opt.Session

		if err := LoadInputs(opt.Inputs, &so); err != nil {
			return nil, err
		}

		if opt.Pull {
			so.FrontendAttrs["image-resolve-mode"] = "pull"
		}
		if opt.Target != "" {
			so.FrontendAttrs["target"] = opt.Target
		}
		if opt.NoCache {
			so.FrontendAttrs["no-cache"] = ""
		}
		for k, v := range opt.BuildArgs {
			so.FrontendAttrs["build-arg:"+k] = v
		}
		for k, v := range opt.Labels {
			so.FrontendAttrs["label:"+k] = v
		}

		if len(opt.Platforms) != 0 {
			pp := make([]string, len(opt.Platforms))
			for i, p := range opt.Platforms {
				pp[i] = platforms.Format(p)
			}
			if len(pp) > 1 && !d.Features()[driver.MultiPlatform] {
				return nil, notSupported(d, driver.MultiPlatform)
			}
			so.FrontendAttrs["platform"] = strings.Join(pp, ",")
		}

		var statusCh chan *client.SolveStatus
		if pw != nil {
			statusCh = pw.Status()
			eg.Go(func() error {
				<-pw.Done()
				return pw.Err()
			})
		}

		eg.Go(func() error {
			rr, err := c.Solve(ctx, nil, so, statusCh)
			if err != nil {
				return err
			}
			mu.Lock()
			resp[k] = rr
			mu.Unlock()
			return nil
		})
	}

	if err := eg.Wait(); err != nil {
		return nil, err
	}

	return resp, nil
}

func LoadInputs(inp Inputs, target *client.SolveOpt) error {
	if inp.ContextPath == "" {
		return errors.New("please specify build context (e.g. \".\" for the current directory)")
	}

	// TODO: handle stdin, symlinks, remote contexts, check files exist

	if inp.DockerfilePath == "" {
		inp.DockerfilePath = filepath.Join(inp.ContextPath, "Dockerfile")
	}

	if target.LocalDirs == nil {
		target.LocalDirs = map[string]string{}
	}

	target.LocalDirs["context"] = inp.ContextPath
	target.LocalDirs["dockerfile"] = filepath.Dir(inp.DockerfilePath)

	if target.FrontendAttrs == nil {
		target.FrontendAttrs = map[string]string{}
	}

	target.FrontendAttrs["filename"] = filepath.Base(inp.DockerfilePath)
	return nil
}

func notSupported(d driver.Driver, f driver.Feature) error {
	return errors.Errorf("%s feature is currently not supported for %s driver. Please switch to a different driver (eg. \"docker buildx new\")", f, d.Factory().Name())
}
