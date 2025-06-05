package types

import (
	"context"
	"io"

	"github.com/docker/buildx/build"
	"github.com/docker/buildx/monitor/processes"
)

// Monitor provides APIs for attaching and controlling the buildx server.
type Monitor interface {
	// Invoke starts an IO session into the specified process.
	// If pid doesn't match to any running processes, it starts a new process with the specified config.
	// If there is no container running or InvokeConfig.Rollback is specified, the process will start in a newly created container.
	// NOTE: If needed, in the future, we can split this API into three APIs (NewContainer, NewProcess and Attach).
	Invoke(ctx context.Context, pid string, options *build.InvokeConfig, ioIn io.ReadCloser, ioOut io.WriteCloser, ioErr io.WriteCloser) error

	ListProcesses(ctx context.Context) (infos []*processes.ProcessInfo, retErr error)

	DisconnectProcess(ctx context.Context, pid string) error

	// Rollback re-runs the interactive container with initial rootfs contents.
	Rollback(ctx context.Context, cfg *build.InvokeConfig) string

	// Rollback executes a process in the interactive container.
	Exec(ctx context.Context, cfg *build.InvokeConfig) string

	// Attach attaches IO to a process in the container.
	Attach(ctx context.Context, pid string)

	// AttachedPID returns ID of the attached process.
	AttachedPID() string

	// Detach detaches IO from the container.
	Detach()

	// Reload will signal the monitor to be reloaded.
	Reload()

	io.Closer
}

// CommandInfo is information about a command.
type CommandInfo struct {
	// Name is the name of the command.
	Name string

	// HelpMessage is one-line message printed to the console when "help" command is invoked.
	HelpMessage string

	// HelpMessageLong is a detailed message printed to the console when "help" command prints this command's information.
	HelpMessageLong string
}

// Command represents a command for debugging.
type Command interface {
	// Exec executes the command.
	Exec(ctx context.Context, args []string) error

	// Info returns information of the command.
	Info() CommandInfo
}
