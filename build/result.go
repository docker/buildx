package build

import (
	"cmp"
	"context"
	_ "crypto/sha256" // ensure digests can be computed
	"encoding/json"
	"io"
	iofs "io/fs"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/moby/buildkit/exporter/containerimage/exptypes"
	gateway "github.com/moby/buildkit/frontend/gateway/client"
	"github.com/moby/buildkit/solver/errdefs"
	"github.com/moby/buildkit/solver/pb"
	ocispecs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/tonistiigi/fsutil/types"
)

// NewResultHandle stores a gateway client, gateway reference, and the error from
// an evaluate call if it is present.
//
// This ResultHandle can be used to execute additional build steps in the same
// context as the build occurred, which can allow easy debugging of build
// failures and successes.
//
// If the returned ResultHandle is not nil, the caller must call Done() on it.
func NewResultHandle(ctx context.Context, c gateway.Client, ref gateway.Reference, meta map[string][]byte, err error) *ResultHandle {
	rCtx := &ResultHandle{
		ref:      ref,
		meta:     meta,
		gwClient: c,
	}
	if err != nil && !errors.As(err, &rCtx.solveErr) {
		return nil
	}
	return rCtx
}

// ResultHandle is a build result with the client that built it.
type ResultHandle struct {
	ref      gateway.Reference
	solveErr *errdefs.SolveError
	meta     map[string][]byte
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

func (r *ResultHandle) StatFile(ctx context.Context, fpath string, cfg *InvokeConfig) (*types.Stat, error) {
	containerCfg, err := r.getContainerConfig(cfg)
	if err != nil {
		return nil, err
	}

	candidateMounts := make([]gateway.Mount, 0, len(containerCfg.Mounts))
	for _, m := range containerCfg.Mounts {
		if strings.HasPrefix(fpath, m.Dest) {
			candidateMounts = append(candidateMounts, m)
		}
	}
	if len(candidateMounts) == 0 {
		return nil, iofs.ErrNotExist
	}

	slices.SortFunc(candidateMounts, func(a, b gateway.Mount) int {
		return cmp.Compare(len(a.Dest), len(b.Dest))
	})

	m := candidateMounts[len(candidateMounts)-1]
	relpath, err := filepath.Rel(m.Dest, fpath)
	if err != nil {
		return nil, err
	}

	if m.Ref == nil {
		return nil, iofs.ErrNotExist
	}

	req := gateway.StatRequest{Path: filepath.ToSlash(relpath)}
	return m.Ref.StatFile(ctx, req)
}

func (r *ResultHandle) getContainerConfig(cfg *InvokeConfig) (containerCfg gateway.NewContainerRequest, _ error) {
	if r.ref != nil && r.solveErr == nil {
		logrus.Debugf("creating container from successful build")
		ccfg, err := containerConfigFromResult(r.ref, cfg)
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
	if r.ref != nil && r.solveErr == nil {
		logrus.Debugf("creating container from successful build")
		if err := populateProcessConfigFromResult(&processCfg, r.meta, cfg); err != nil {
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

func containerConfigFromResult(ref gateway.Reference, cfg *InvokeConfig) (*gateway.NewContainerRequest, error) {
	if cfg.Initial {
		return nil, errors.Errorf("starting from the container from the initial state of the step is supported only on the failed steps")
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

func populateProcessConfigFromResult(req *gateway.StartRequest, meta map[string][]byte, cfg *InvokeConfig) error {
	imgData := meta[exptypes.ExporterImageConfigKey]
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
