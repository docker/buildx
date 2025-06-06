package build

import (
	"context"
	_ "crypto/sha256" // ensure digests can be computed
	"encoding/json"
	"io"
	"sync"

	"github.com/moby/buildkit/exporter/containerimage/exptypes"
	gateway "github.com/moby/buildkit/frontend/gateway/client"
	"github.com/moby/buildkit/solver/errdefs"
	"github.com/moby/buildkit/solver/pb"
	"github.com/moby/buildkit/solver/result"
	ocispecs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"golang.org/x/sync/errgroup"
)

// NewResultHandle makes a call to client.Build, additionally returning a
// opaque ResultHandle alongside the standard response and error.
//
// This ResultHandle can be used to execute additional build steps in the same
// context as the build occurred, which can allow easy debugging of build
// failures and successes.
//
// If the returned ResultHandle is not nil, the caller must call Done() on it.
func NewResultHandle(ctx context.Context, c gateway.Client, res *gateway.Result, err error) *ResultHandle {
	rCtx := &ResultHandle{
		res:      res,
		gwClient: c,
	}
	errors.As(err, &rCtx.solveErr)
	return rCtx
}

// getDefinition converts a gateway result into a collection of definitions for
// each ref in the result.
func getDefinition(ctx context.Context, res *gateway.Result) (*result.Result[*pb.Definition], error) {
	return result.ConvertResult(res, func(ref gateway.Reference) (*pb.Definition, error) {
		st, err := ref.ToState()
		if err != nil {
			return nil, err
		}
		def, err := st.Marshal(ctx)
		if err != nil {
			return nil, err
		}
		return def.ToPB(), nil
	})
}

// evalDefinition performs the reverse of getDefinition, converting a
// collection of definitions into a gateway result.
func evalDefinition(ctx context.Context, c gateway.Client, defs *result.Result[*pb.Definition]) (*gateway.Result, error) {
	// force evaluation of all targets in parallel
	results := make(map[*pb.Definition]*gateway.Result)
	resultsMu := sync.Mutex{}
	eg, egCtx := errgroup.WithContext(ctx)
	defs.EachRef(func(def *pb.Definition) error {
		eg.Go(func() error {
			res, err := c.Solve(egCtx, gateway.SolveRequest{
				Evaluate:   true,
				Definition: def,
			})
			if err != nil {
				return err
			}
			resultsMu.Lock()
			results[def] = res
			resultsMu.Unlock()
			return nil
		})
		return nil
	})
	if err := eg.Wait(); err != nil {
		return nil, err
	}
	res, _ := result.ConvertResult(defs, func(def *pb.Definition) (gateway.Reference, error) {
		if res, ok := results[def]; ok {
			return res.Ref, nil
		}
		return nil, nil
	})
	return res, nil
}

// ResultHandle is a build result with the client that built it.
type ResultHandle struct {
	res      *gateway.Result
	solveErr *errdefs.SolveError
	gwClient gateway.Client

	doneOnce sync.Once

	cleanups   []func()
	cleanupsMu sync.Mutex
}

func (r *ResultHandle) Done() {
	r.doneOnce.Do(func() {
		r.cleanupsMu.Lock()
		cleanups := r.cleanups
		r.cleanups = nil
		r.cleanupsMu.Unlock()
		for _, f := range cleanups {
			f()
		}
	})
}

func (r *ResultHandle) registerCleanup(f func()) {
	r.cleanupsMu.Lock()
	r.cleanups = append(r.cleanups, f)
	r.cleanupsMu.Unlock()
}

func (r *ResultHandle) NewContainer(ctx context.Context, cfg *InvokeConfig) (gateway.Container, error) {
	req, err := r.getContainerConfig(cfg)
	if err != nil {
		return nil, err
	}
	return r.gwClient.NewContainer(ctx, req)
}

func (r *ResultHandle) getContainerConfig(cfg *InvokeConfig) (containerCfg gateway.NewContainerRequest, _ error) {
	if r.res != nil && r.solveErr == nil {
		logrus.Debugf("creating container from successful build")
		ccfg, err := containerConfigFromResult(r.res, cfg)
		if err != nil {
			return containerCfg, err
		}
		containerCfg = *ccfg
	} else {
		logrus.Debugf("creating container from failed build %+v", cfg)
		ccfg, err := containerConfigFromError(r.solveErr, cfg)
		if err != nil {
			return containerCfg, errors.Wrapf(err, "no result nor error is available")
		}
		containerCfg = *ccfg
	}
	return containerCfg, nil
}

func (r *ResultHandle) getProcessConfig(cfg *InvokeConfig, stdin io.ReadCloser, stdout io.WriteCloser, stderr io.WriteCloser) (_ gateway.StartRequest, err error) {
	processCfg := newStartRequest(stdin, stdout, stderr)
	if r.res != nil && r.solveErr == nil {
		logrus.Debugf("creating container from successful build")
		if err := populateProcessConfigFromResult(&processCfg, r.res, cfg); err != nil {
			return processCfg, err
		}
	} else {
		logrus.Debugf("creating container from failed build %+v", cfg)
		if err := populateProcessConfigFromError(&processCfg, r.solveErr, cfg); err != nil {
			return processCfg, err
		}
	}
	return processCfg, nil
}

func containerConfigFromResult(res *gateway.Result, cfg *InvokeConfig) (*gateway.NewContainerRequest, error) {
	if cfg.Initial {
		return nil, errors.Errorf("starting from the container from the initial state of the step is supported only on the failed steps")
	}

	ps, err := exptypes.ParsePlatforms(res.Metadata)
	if err != nil {
		return nil, err
	}
	ref, ok := res.FindRef(ps.Platforms[0].ID)
	if !ok {
		return nil, errors.Errorf("no reference found")
	}

	return &gateway.NewContainerRequest{
		Mounts: []gateway.Mount{
			{
				Dest:      "/",
				MountType: pb.MountType_BIND,
				Ref:       ref,
			},
		},
	}, nil
}

func populateProcessConfigFromResult(req *gateway.StartRequest, res *gateway.Result, cfg *InvokeConfig) error {
	imgData := res.Metadata[exptypes.ExporterImageConfigKey]
	var img *ocispecs.Image
	if len(imgData) > 0 {
		img = &ocispecs.Image{}
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
	if !cfg.NoCmd {
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

func containerConfigFromError(solveErr *errdefs.SolveError, cfg *InvokeConfig) (*gateway.NewContainerRequest, error) {
	exec, err := execOpFromError(solveErr)
	if err != nil {
		return nil, err
	}
	var mounts []gateway.Mount
	for i, mnt := range exec.Mounts {
		rid := solveErr.MountIDs[i]
		if cfg.Initial {
			rid = solveErr.InputIDs[i]
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

func populateProcessConfigFromError(req *gateway.StartRequest, solveErr *errdefs.SolveError, cfg *InvokeConfig) error {
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
	switch op := solveErr.Op.GetOp().(type) {
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
