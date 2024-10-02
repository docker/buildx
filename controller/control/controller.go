package control

import (
	"context"
	"io"

	"github.com/docker/buildx/build"
	controllerapi "github.com/docker/buildx/controller/pb"
	"github.com/docker/buildx/util/progress"
	"github.com/moby/buildkit/client"
)

type BuildxController interface {
	Build(ctx context.Context, options *controllerapi.BuildOptions, in io.ReadCloser, progress progress.Writer) (ref string, resp *client.SolveResponse, inputs *build.Inputs, err error)
	// Invoke starts an IO session into the specified process.
	// If pid doesn't matche to any running processes, it starts a new process with the specified config.
	// If there is no container running or InvokeConfig.Rollback is speicfied, the process will start in a newly created container.
	// NOTE: If needed, in the future, we can split this API into three APIs (NewContainer, NewProcess and Attach).
	Invoke(ctx context.Context, ref, pid string, options *controllerapi.InvokeConfig, ioIn io.ReadCloser, ioOut io.WriteCloser, ioErr io.WriteCloser) error
	Kill(ctx context.Context) error
	Close() error
	List(ctx context.Context) (refs []string, _ error)
	Disconnect(ctx context.Context, ref string) error
	ListProcesses(ctx context.Context, ref string) (infos []*controllerapi.ProcessInfo, retErr error)
	DisconnectProcess(ctx context.Context, ref, pid string) error
	Inspect(ctx context.Context, ref string) (*controllerapi.InspectResponse, error)
}

type ControlOptions struct {
	ServerConfig string
	Root         string
	Detach       bool
}
