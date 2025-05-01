package control

import (
	"context"
	"io"
	"time"

	"github.com/docker/buildx/build"
	cbuild "github.com/docker/buildx/controller/build"
	controllerapi "github.com/docker/buildx/controller/pb"
	"github.com/docker/buildx/controller/processes"
	"github.com/docker/buildx/util/progress"
	"github.com/moby/buildkit/client"
)

type BuildxController interface {
	Build(ctx context.Context, options *ControlOptions, in io.ReadCloser, progress progress.Writer) (resp *client.SolveResponse, inputs *build.Inputs, err error)
	// Invoke starts an IO session into the specified process.
	// If pid doesn't match to any running processes, it starts a new process with the specified config.
	// If there is no container running or InvokeConfig.Rollback is specified, the process will start in a newly created container.
	// NOTE: If needed, in the future, we can split this API into three APIs (NewContainer, NewProcess and Attach).
	Invoke(ctx context.Context, pid string, options *controllerapi.InvokeConfig, ioIn io.ReadCloser, ioOut io.WriteCloser, ioErr io.WriteCloser) error
	Close() error
	ListProcesses(ctx context.Context) (infos []*processes.ProcessInfo, retErr error)
	DisconnectProcess(ctx context.Context, pid string) error
	Inspect(ctx context.Context) *ControlOptions
}

type ControlOptions struct {
	cbuild.Options
	Timeout      time.Duration
}
