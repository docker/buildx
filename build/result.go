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
	"github.com/moby/buildkit/solver/result"
	specs "github.com/opencontainers/image-spec/specs-go/v1"
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
func NewResultHandle(ctx context.Context, cc *client.Client, opt client.SolveOpt, product string, buildFunc gateway.BuildFunc, ch chan *client.SolveStatus) (*ResultHandle, *client.SolveResponse, error) {
	// Create a new context to wrap the original, and cancel it when the
	// caller-provided context is cancelled.
	//
	// We derive the context from the background context so that we can forbid
	// cancellation of the build request after <-done is closed (which we do
	// before returning the ResultHandle).
	baseCtx := ctx
	ctx, cancel := context.WithCancelCause(context.Background())
	done := make(chan struct{})
	go func() {
		select {
		case <-baseCtx.Done():
			cancel(baseCtx.Err())
		case <-done:
			// Once done is closed, we've recorded a ResultHandle, so we
			// shouldn't allow cancelling the underlying build request anymore.
		}
	}()

	// Create a new channel to forward status messages to the original.
	//
	// We do this so that we can discard status messages after the main portion
	// of the build is complete. This is necessary for the solve error case,
	// where the original gateway is kept open until the ResultHandle is
	// closed - we don't want progress messages from operations in that
	// ResultHandle to display after this function exits.
	//
	// Additionally, callers should wait for the progress channel to be closed.
	// If we keep the session open and never close the progress channel, the
	// caller will likely hang.
	baseCh := ch
	ch = make(chan *client.SolveStatus)
	go func() {
		for {
			s, ok := <-ch
			if !ok {
				return
			}
			select {
			case <-baseCh:
				// base channel is closed, discard status messages
			default:
				baseCh <- s
			}
		}
	}()
	defer close(baseCh)

	var resp *client.SolveResponse
	var respErr error
	var respHandle *ResultHandle

	go func() {
		defer func() { cancel(errors.WithStack(context.Canceled)) }() // ensure no dangling processes

		var res *gateway.Result
		var err error
		resp, err = cc.Build(ctx, opt, product, func(ctx context.Context, c gateway.Client) (*gateway.Result, error) {
			var err error
			res, err = buildFunc(ctx, c)

			if res != nil && err == nil {
				// Force evaluation of the build result (otherwise, we likely
				// won't get a solve error)
				def, err2 := getDefinition(ctx, res)
				if err2 != nil {
					return nil, err2
				}
				res, err = evalDefinition(ctx, c, def)
			}

			if err != nil {
				// Scenario 1: we failed to evaluate a node somewhere in the
				// build graph.
				//
				// In this case, we construct a ResultHandle from this
				// original Build session, and return it alongside the original
				// build error. We then need to keep the gateway session open
				// until the caller explicitly closes the ResultHandle.

				var se *errdefs.SolveError
				if errors.As(err, &se) {
					respHandle = &ResultHandle{
						done:     make(chan struct{}),
						solveErr: se,
						gwClient: c,
						gwCtx:    ctx,
					}
					respErr = err // return original error to preserve stacktrace
					close(done)

					// Block until the caller closes the ResultHandle.
					select {
					case <-respHandle.done:
					case <-ctx.Done():
					}
				}
			}
			return res, err
		}, ch)
		if respHandle != nil {
			return
		}
		if err != nil {
			// Something unexpected failed during the build, we didn't succeed,
			// but we also didn't make it far enough to create a ResultHandle.
			respErr = err
			close(done)
			return
		}

		// Scenario 2: we successfully built the image with no errors.
		//
		// In this case, the original gateway session has now been closed
		// since the Build has been completed. So, we need to create a new
		// gateway session to populate the ResultHandle. To do this, we
		// need to re-evaluate the target result, in this new session. This
		// should be instantaneous since the result should be cached.

		def, err := getDefinition(ctx, res)
		if err != nil {
			respErr = err
			close(done)
			return
		}

		// NOTE: ideally this second connection should be lazily opened
		opt := opt
		opt.Ref = ""
		opt.Exports = nil
		opt.CacheExports = nil
		opt.Internal = true
		_, respErr = cc.Build(ctx, opt, "buildx", func(ctx context.Context, c gateway.Client) (*gateway.Result, error) {
			res, err := evalDefinition(ctx, c, def)
			if err != nil {
				// This should probably not happen, since we've previously
				// successfully evaluated the same result with no issues.
				return nil, errors.Wrap(err, "inconsistent solve result")
			}
			respHandle = &ResultHandle{
				done:     make(chan struct{}),
				res:      res,
				gwClient: c,
				gwCtx:    ctx,
			}
			close(done)

			// Block until the caller closes the ResultHandle.
			select {
			case <-respHandle.done:
			case <-ctx.Done():
			}
			return nil, context.Cause(ctx)
		}, nil)
		if respHandle != nil {
			return
		}
		close(done)
	}()

	// Block until the other thread signals that it's completed the build.
	select {
	case <-done:
	case <-baseCtx.Done():
		if respErr == nil {
			respErr = baseCtx.Err()
		}
	}
	return respHandle, resp, respErr
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

	done     chan struct{}
	doneOnce sync.Once

	gwClient gateway.Client
	gwCtx    context.Context

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

		close(r.done)
		<-r.gwCtx.Done()
	})
}

func (r *ResultHandle) registerCleanup(f func()) {
	r.cleanupsMu.Lock()
	r.cleanups = append(r.cleanups, f)
	r.cleanupsMu.Unlock()
}

func (r *ResultHandle) build(buildFunc gateway.BuildFunc) (err error) {
	_, err = buildFunc(r.gwCtx, r.gwClient)
	return err
}

func (r *ResultHandle) getContainerConfig(cfg *controllerapi.InvokeConfig) (containerCfg gateway.NewContainerRequest, _ error) {
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

func (r *ResultHandle) getProcessConfig(cfg *controllerapi.InvokeConfig, stdin io.ReadCloser, stdout io.WriteCloser, stderr io.WriteCloser) (_ gateway.StartRequest, err error) {
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

func containerConfigFromResult(res *gateway.Result, cfg *controllerapi.InvokeConfig) (*gateway.NewContainerRequest, error) {
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

func populateProcessConfigFromResult(req *gateway.StartRequest, res *gateway.Result, cfg *controllerapi.InvokeConfig) error {
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

func containerConfigFromError(solveErr *errdefs.SolveError, cfg *controllerapi.InvokeConfig) (*gateway.NewContainerRequest, error) {
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

func populateProcessConfigFromError(req *gateway.StartRequest, solveErr *errdefs.SolveError, cfg *controllerapi.InvokeConfig) error {
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
