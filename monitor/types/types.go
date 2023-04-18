package types

import (
	"context"
	"io"

	controllerapi "github.com/docker/buildx/controller/pb"
	"github.com/docker/buildx/util/progress"
	"github.com/moby/buildkit/client"
)

// Monitor provides APIs for attaching and controlling the buildx server.
type Monitor interface {
	// Rollback re-runs the interactive container with initial rootfs contents.
	Rollback(ctx context.Context, cfg controllerapi.InvokeConfig) string

	// Rollback executes a process in the interactive container.
	Exec(ctx context.Context, cfg controllerapi.InvokeConfig) string

	// Attach attaches IO to a process in the container.
	Attach(ctx context.Context, pid string)

	// AttachedPID returns ID of the attached process.
	AttachedPID() string

	// Detach detaches IO from the container.
	Detach()

	// DisconnectSession finishes the specified session.
	DisconnectSession(ctx context.Context, targetID string) error

	// AttachSession attaches the monitor to the specified session.
	AttachSession(ref string)

	// AttachedSessionID returns the ID of the attached session.
	AttachedSessionID() string

	// Build executes the specified build and returns an ID of the session for debugging that build.
	Build(ctx context.Context, options controllerapi.BuildOptions, in io.ReadCloser, progress progress.Writer) (ref string, resp *client.SolveResponse, err error)

	// Kill kills the buildx server.
	Kill(ctx context.Context) error

	// List lists sessions.
	List(ctx context.Context) (refs []string, _ error)

	// ListPrcesses lists processes in the attached session.
	ListProcesses(ctx context.Context) (infos []*controllerapi.ProcessInfo, retErr error)

	// DisconnectProcess finishes the specified process.
	DisconnectProcess(ctx context.Context, pid string) error

	// Inspect returns information about the attached build.
	Inspect(ctx context.Context) (*controllerapi.InspectResponse, error)

	// Disconnect finishes the attached session.
	Disconnect(ctx context.Context) error
}

// CommandInfo is information about a command.
type CommandInfo struct {

	// HelpMessage is the message printed to the console when "help" command is invoked.
	HelpMessage string
}

// Command represents a command for debugging.
type Command interface {

	// Exec executes the command.
	Exec(ctx context.Context, args []string) error

	// Info returns information of the command.
	Info() CommandInfo
}
