package types

import (
	"context"

	"github.com/docker/buildx/controller/control"
	controllerapi "github.com/docker/buildx/controller/pb"
)

// Monitor provides APIs for attaching and controlling the buildx server.
type Monitor interface {
	control.BuildxController

	// Rollback re-runs the interactive container with initial rootfs contents.
	Rollback(ctx context.Context, cfg *controllerapi.InvokeConfig) string

	// Rollback executes a process in the interactive container.
	Exec(ctx context.Context, cfg *controllerapi.InvokeConfig) string

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
