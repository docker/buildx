package local

import (
	"context"
	"io"
	"sync/atomic"

	"github.com/docker/buildx/build"
	cbuild "github.com/docker/buildx/controller/build"
	controllererrors "github.com/docker/buildx/controller/errdefs"
	controllerapi "github.com/docker/buildx/controller/pb"
	"github.com/docker/buildx/controller/processes"
	"github.com/docker/buildx/util/ioset"
	"github.com/docker/buildx/util/progress"
	"github.com/docker/cli/cli/command"
	"github.com/moby/buildkit/client"
	"github.com/pkg/errors"
)

func NewController(ctx context.Context, dockerCli command.Cli) *Controller {
	return &Controller{
		dockerCli: dockerCli,
	}
}

type buildConfig struct {
	// TODO: these two structs should be merged
	// Discussion: https://github.com/docker/buildx/pull/1640#discussion_r1113279719
	resultCtx    *build.ResultHandle
	buildOptions *cbuild.Options
}

type Controller struct {
	dockerCli   command.Cli
	buildConfig buildConfig

	buildOnGoing atomic.Bool
}

func (b *Controller) Build(ctx context.Context, options *cbuild.Options, in io.ReadCloser, progress progress.Writer) (*client.SolveResponse, *build.Inputs, error) {
	if !b.buildOnGoing.CompareAndSwap(false, true) {
		return nil, nil, errors.New("build ongoing")
	}
	defer b.buildOnGoing.Store(false)

	resp, res, dockerfileMappings, buildErr := cbuild.RunBuild(ctx, b.dockerCli, options, in, progress, true)
	// NOTE: RunBuild can return *build.ResultHandle even on error.
	if res != nil {
		b.buildConfig = buildConfig{
			resultCtx:    res,
			buildOptions: options,
		}
		if buildErr != nil {
			buildErr = controllererrors.WrapBuild(buildErr)
		}
	}
	if buildErr != nil {
		return nil, nil, buildErr
	}
	return resp, dockerfileMappings, nil
}

func (b *Controller) Invoke(ctx context.Context, processes *processes.Manager, pid string, cfg *controllerapi.InvokeConfig, ioIn io.ReadCloser, ioOut io.WriteCloser, ioErr io.WriteCloser) error {
	proc, ok := processes.Get(pid)
	if !ok {
		// Start a new process.
		if b.buildConfig.resultCtx == nil {
			return errors.New("no build result is registered")
		}
		var err error
		proc, err = processes.StartProcess(pid, b.buildConfig.resultCtx, cfg)
		if err != nil {
			return err
		}
	}

	// Attach containerIn to this process
	ioCancelledCh := make(chan struct{})
	proc.ForwardIO(&ioset.In{Stdin: ioIn, Stdout: ioOut, Stderr: ioErr}, func(error) { close(ioCancelledCh) })

	select {
	case <-ioCancelledCh:
		return errors.Errorf("io cancelled")
	case err := <-proc.Done():
		return err
	case <-ctx.Done():
		return context.Cause(ctx)
	}
}

func (b *Controller) Close() error {
	if b.buildConfig.resultCtx != nil {
		b.buildConfig.resultCtx.Done()
	}
	return nil
}
