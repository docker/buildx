package build

import (
	"context"
	"io"
	"path/filepath"
	"strconv"
	"strings"

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

func Build(ctx context.Context, drivers []driver.Driver, opt Options, pw progress.Writer) (*client.SolveResponse, error) {
	if len(drivers) == 0 {
		return nil, errors.Errorf("driver required for build")
	}

	if len(drivers) > 1 {
		return nil, errors.Errorf("multiple drivers currently not supported")
	}

	pwOld := pw
	c, pw, err := driver.Boot(ctx, drivers[0], pw)
	if err != nil {
		close(pwOld.Status())
		<-pwOld.Done()
		return nil, err
	}

	so := client.SolveOpt{
		Frontend:      "dockerfile.v0",
		FrontendAttrs: map[string]string{},
	}

	if len(opt.Exports) > 1 {
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
		so.FrontendAttrs["platform"] = strings.Join(pp, ",")
	}

	eg, ctx := errgroup.WithContext(ctx)

	var statusCh chan *client.SolveStatus
	if pw != nil {
		statusCh = pw.Status()
		eg.Go(func() error {
			<-pw.Done()
			return pw.Err()
		})
	}

	var resp *client.SolveResponse
	eg.Go(func() error {
		var err error
		resp, err = c.Solve(ctx, nil, so, statusCh)
		if err != nil {
			return err
		}
		return nil
	})

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
