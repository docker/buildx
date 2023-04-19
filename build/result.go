package build

import (
	"context"
	_ "crypto/sha256" // ensure digests can be computed
	"encoding/json"
	"io"
	"sync"

	controllerapi "github.com/docker/buildx/controller/pb"
	"github.com/moby/buildkit/client"
	"github.com/moby/buildkit/exporter/containerimage/exptypes"
	gateway "github.com/moby/buildkit/frontend/gateway/client"
	"github.com/moby/buildkit/solver/errdefs"
	"github.com/moby/buildkit/solver/pb"
	specs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"golang.org/x/sync/errgroup"
)

// ResultContext is a build result with the client that built it.
type ResultContext struct {
	resp        *client.SolveResponse
	err         error
	suspend     chan<- struct{}
	suspendDone <-chan struct{}

	hooks []func(ctx context.Context) error

	gwRef *gateway.Result
	gwErr *errdefs.SolveError

	gwClient   gateway.Client
	gwCtx      context.Context
	gwDone     func()
	gwDoneOnce sync.Once

	cleanups   []func()
	cleanupsMu sync.Mutex
}

func (r *ResultContext) hook(hook func(ctx context.Context) error) {
	r.hooks = append(r.hooks, hook)
}

func (r *ResultContext) Result(ctx context.Context) (*gateway.Result, *errdefs.SolveError) {
	return r.gwRef, r.gwErr
}

func (r *ResultContext) Wait(ctx context.Context) (*client.SolveResponse, error) {
	defer r.Close()

	close(r.suspend)
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-r.suspendDone:
		eg, ctx := errgroup.WithContext(ctx)
		eg.Go(func() error {
			for _, f := range r.hooks {
				if err := f(ctx); err != nil {
					return err
				}
			}
			return nil
		})
		if err := eg.Wait(); err != nil {
			return nil, err
		}
		return r.resp, r.err
	}
}

func (r *ResultContext) Close() {
	r.gwDoneOnce.Do(func() {
		r.cleanupsMu.Lock()
		cleanups := r.cleanups
		r.cleanups = nil
		r.cleanupsMu.Unlock()
		for _, f := range cleanups {
			f()
		}
		r.gwDone()
	})
}

func (r *ResultContext) registerCleanup(f func()) {
	r.cleanupsMu.Lock()
	r.cleanups = append(r.cleanups, f)
	r.cleanupsMu.Unlock()
}

func (r *ResultContext) build(buildFunc gateway.BuildFunc) (err error) {
	_, err = buildFunc(r.gwCtx, r.gwClient)
	return err
}

func (r *ResultContext) getContainerConfig(ctx context.Context, c gateway.Client, cfg *controllerapi.InvokeConfig) (containerCfg gateway.NewContainerRequest, _ error) {
	if r.gwRef != nil && r.gwErr == nil {
		logrus.Debugf("creating container from successful build")
		ccfg, err := containerConfigFromResult(ctx, r.gwRef, c, *cfg)
		if err != nil {
			return containerCfg, err
		}
		containerCfg = *ccfg
	} else {
		logrus.Debugf("creating container from failed build %+v", cfg)
		ccfg, err := containerConfigFromError(r.gwErr, *cfg)
		if err != nil {
			return containerCfg, errors.Wrapf(err, "no result nor error is available")
		}
		containerCfg = *ccfg
	}
	return containerCfg, nil
}

func (r *ResultContext) getProcessConfig(cfg *controllerapi.InvokeConfig, stdin io.ReadCloser, stdout io.WriteCloser, stderr io.WriteCloser) (_ gateway.StartRequest, err error) {
	processCfg := newStartRequest(stdin, stdout, stderr)
	if r.gwRef != nil && r.gwErr == nil {
		logrus.Debugf("creating container from successful build")
		if err := populateProcessConfigFromResult(&processCfg, r.gwRef, *cfg); err != nil {
			return processCfg, err
		}
	} else {
		logrus.Debugf("creating container from failed build %+v", cfg)
		if err := populateProcessConfigFromError(&processCfg, r.gwErr, *cfg); err != nil {
			return processCfg, err
		}
	}
	return processCfg, nil
}

func containerConfigFromResult(ctx context.Context, res *gateway.Result, c gateway.Client, cfg controllerapi.InvokeConfig) (*gateway.NewContainerRequest, error) {
	if res.Ref == nil {
		return nil, errors.Errorf("no reference is registered")
	}
	if cfg.Initial {
		return nil, errors.Errorf("starting from the container from the initial state of the step is supported only on the failed steps")
	}
	st, err := res.Ref.ToState()
	if err != nil {
		return nil, err
	}
	def, err := st.Marshal(ctx)
	if err != nil {
		return nil, err
	}
	imgRef, err := c.Solve(ctx, gateway.SolveRequest{
		Definition: def.ToPB(),
	})
	if err != nil {
		return nil, err
	}
	return &gateway.NewContainerRequest{
		Mounts: []gateway.Mount{
			{
				Dest:      "/",
				MountType: pb.MountType_BIND,
				Ref:       imgRef.Ref,
			},
		},
	}, nil
}

func populateProcessConfigFromResult(req *gateway.StartRequest, res *gateway.Result, cfg controllerapi.InvokeConfig) error {
	imgData := res.Metadata[exptypes.ExporterImageConfigKey]
	var img *specs.Image
	if len(imgData) > 0 {
		img = &specs.Image{}
		if err := json.Unmarshal(imgData, img); err != nil {
			return err
		}
	}

	user := ""
	if !cfg.NoUser {
		user = cfg.User
	} else if img != nil {
		user = img.Config.User
	}

	cwd := ""
	if !cfg.NoCwd {
		cwd = cfg.Cwd
	} else if img != nil {
		cwd = img.Config.WorkingDir
	}

	env := []string{}
	if img != nil {
		env = append(env, img.Config.Env...)
	}
	env = append(env, cfg.Env...)

	args := []string{}
	if cfg.Entrypoint != nil {
		args = append(args, cfg.Entrypoint...)
	} else if img != nil {
		args = append(args, img.Config.Entrypoint...)
	}
	if cfg.Cmd != nil {
		args = append(args, cfg.Cmd...)
	} else if img != nil {
		args = append(args, img.Config.Cmd...)
	}

	req.Args = args
	req.Env = env
	req.User = user
	req.Cwd = cwd
	req.Tty = cfg.Tty

	return nil
}

func containerConfigFromError(solveErr *errdefs.SolveError, cfg controllerapi.InvokeConfig) (*gateway.NewContainerRequest, error) {
	exec, err := execOpFromError(solveErr)
	if err != nil {
		return nil, err
	}
	var mounts []gateway.Mount
	for i, mnt := range exec.Mounts {
		rid := solveErr.Solve.MountIDs[i]
		if cfg.Initial {
			rid = solveErr.Solve.InputIDs[i]
		}
		mounts = append(mounts, gateway.Mount{
			Selector:  mnt.Selector,
			Dest:      mnt.Dest,
			ResultID:  rid,
			Readonly:  mnt.Readonly,
			MountType: mnt.MountType,
			CacheOpt:  mnt.CacheOpt,
			SecretOpt: mnt.SecretOpt,
			SSHOpt:    mnt.SSHOpt,
		})
	}
	return &gateway.NewContainerRequest{
		Mounts:  mounts,
		NetMode: exec.Network,
	}, nil
}

func populateProcessConfigFromError(req *gateway.StartRequest, solveErr *errdefs.SolveError, cfg controllerapi.InvokeConfig) error {
	exec, err := execOpFromError(solveErr)
	if err != nil {
		return err
	}
	meta := exec.Meta
	user := ""
	if !cfg.NoUser {
		user = cfg.User
	} else {
		user = meta.User
	}

	cwd := ""
	if !cfg.NoCwd {
		cwd = cfg.Cwd
	} else {
		cwd = meta.Cwd
	}

	env := append(meta.Env, cfg.Env...)

	args := []string{}
	if cfg.Entrypoint != nil {
		args = append(args, cfg.Entrypoint...)
	}
	if cfg.Cmd != nil {
		args = append(args, cfg.Cmd...)
	}
	if len(args) == 0 {
		args = meta.Args
	}

	req.Args = args
	req.Env = env
	req.User = user
	req.Cwd = cwd
	req.Tty = cfg.Tty

	return nil
}

func execOpFromError(solveErr *errdefs.SolveError) (*pb.ExecOp, error) {
	if solveErr == nil {
		return nil, errors.Errorf("no error is available")
	}
	switch op := solveErr.Solve.Op.GetOp().(type) {
	case *pb.Op_Exec:
		return op.Exec, nil
	default:
		return nil, errors.Errorf("invoke: unsupported error type")
	}
	// TODO: support other ops
}

func newStartRequest(stdin io.ReadCloser, stdout io.WriteCloser, stderr io.WriteCloser) gateway.StartRequest {
	return gateway.StartRequest{
		Stdin:  stdin,
		Stdout: stdout,
		Stderr: stderr,
	}
}
