package driver

import (
	"context"

	"github.com/moby/buildkit/client"
	specs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/pkg/errors"
)

var ErrNotRunning = errors.Errorf("driver not running")
var ErrNotConnecting = errors.Errorf("driver not connection")

type Logger func(*client.SolveStatus)

type Status int

const (
	Terminated Status = iota
	Starting
	Running
	Stopping
	Stopped
)

type Info struct {
	Status    Status
	Platforms []specs.Platform
}

type Driver interface {
	Bootstrap(context.Context, Logger) error
	Info(context.Context) (Info, error)
	Stop(ctx context.Context, force bool) error
	Rm(ctx context.Context, force bool) error
	Client() (*client.Client, error)
}
