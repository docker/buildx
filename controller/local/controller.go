package local

import (
	"context"
	"io"
	"sync/atomic"

	"github.com/docker/buildx/build"
	cbuild "github.com/docker/buildx/controller/build"
	"github.com/docker/buildx/controller/control"
	controllererrors "github.com/docker/buildx/controller/errdefs"
	controllerapi "github.com/docker/buildx/controller/pb"
	"github.com/docker/buildx/controller/processes"
	"github.com/docker/buildx/util/ioset"
	"github.com/docker/buildx/util/progress"
	"github.com/docker/cli/cli/command"
	"github.com/moby/buildkit/client"
	"github.com/pkg/errors"
)

func NewLocalBuildxController(ctx context.Context, dockerCli command.Cli, logger progress.SubLogger) control.BuildxController {
	return &localController{
		dockerCli: dockerCli,
		ref:       "local",
		processes: processes.NewManager(),
	}
}

type buildConfig struct {
	// TODO: these two structs should be merged
	// Discussion: https://github.com/docker/buildx/pull/1640#discussion_r1113279719
	resultCtx    *build.ResultHandle
	buildOptions *controllerapi.BuildOptions
}

type localController struct {
	dockerCli   command.Cli
	ref         string
	buildConfig buildConfig
	processes   *processes.Manager

	buildOnGoing atomic.Bool
}

func (b *localController) Build(ctx context.Context, options controllerapi.BuildOptions, in io.ReadCloser, progress progress.Writer) (string, *client.SolveResponse, map[string]string, error) {
	if !b.buildOnGoing.CompareAndSwap(false, true) {
		return "", nil, nil, errors.New("build ongoing")
	}
	defer b.buildOnGoing.Store(false)

	resp, res, dockerfileMappings, buildErr := cbuild.RunBuild(ctx, b.dockerCli, options, in, progress, true)
	// NOTE: RunBuild can return *build.ResultHandle even on error.
	if res != nil {
		b.buildConfig = buildConfig{
			resultCtx:    res,
			buildOptions: &options,
		}
		if buildErr != nil {
			buildErr = controllererrors.WrapBuild(buildErr, b.ref)
		}
	}
	if buildErr != nil {
		return "", nil, nil, buildErr
	}
	return b.ref, resp, dockerfileMappings, nil
}

func (b *localController) ListProcesses(ctx context.Context, ref string) (infos []*controllerapi.ProcessInfo, retErr error) {
	if ref != b.ref {
		return nil, errors.Errorf("unknown ref %q", ref)
	}
	return b.processes.ListProcesses(), nil
}

func (b *localController) DisconnectProcess(ctx context.Context, ref, pid string) error {
	if ref != b.ref {
		return errors.Errorf("unknown ref %q", ref)
	}
	return b.processes.DeleteProcess(pid)
}

func (b *localController) cancelRunningProcesses() {
	b.processes.CancelRunningProcesses()
}

func (b *localController) Invoke(ctx context.Context, ref string, pid string, cfg controllerapi.InvokeConfig, ioIn io.ReadCloser, ioOut io.WriteCloser, ioErr io.WriteCloser) error {
	if ref != b.ref {
		return errors.Errorf("unknown ref %q", ref)
	}

	proc, ok := b.processes.Get(pid)
	if !ok {
		// Start a new process.
		if b.buildConfig.resultCtx == nil {
			return errors.New("no build result is registered")
		}
		var err error
		proc, err = b.processes.StartProcess(pid, b.buildConfig.resultCtx, &cfg)
		if err != nil {
			return err
		}
	}

	// Attach containerIn to this process
	ioCancelledCh := make(chan struct{})
	proc.ForwardIO(&ioset.In{Stdin: ioIn, Stdout: ioOut, Stderr: ioErr}, func() { close(ioCancelledCh) })

	select {
	case <-ioCancelledCh:
		return errors.Errorf("io cancelled")
	case err := <-proc.Done():
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (b *localController) Kill(context.Context) error {
	b.Close()
	return nil
}

func (b *localController) Close() error {
	b.cancelRunningProcesses()
	if b.buildConfig.resultCtx != nil {
		b.buildConfig.resultCtx.Done()
	}
	// TODO: cancel ongoing builds?
	return nil
}

func (b *localController) List(ctx context.Context) (res []string, _ error) {
	return []string{b.ref}, nil
}

func (b *localController) Disconnect(ctx context.Context, key string) error {
	b.Close()
	return nil
}

func (b *localController) Inspect(ctx context.Context, ref string) (*controllerapi.InspectResponse, error) {
	if ref != b.ref {
		return nil, errors.Errorf("unknown ref %q", ref)
	}
	return &controllerapi.InspectResponse{Options: b.buildConfig.buildOptions}, nil
}
