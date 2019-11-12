package driver

import (
	"context"

	"github.com/docker/buildx/store"
	"github.com/docker/buildx/util/progress"
	"github.com/moby/buildkit/client"
	"github.com/pkg/errors"
)

var ErrNotRunning = errors.Errorf("driver not running")
var ErrNotConnecting = errors.Errorf("driver not connecting")

type Status int

const (
	Inactive Status = iota
	Starting
	Running
	Stopping
	Stopped
)

func (s Status) String() string {
	switch s {
	case Inactive:
		return "inactive"
	case Starting:
		return "starting"
	case Running:
		return "running"
	case Stopping:
		return "stopping"
	case Stopped:
		return "stopped"
	}
	return "unknown"
}

type Info struct {
	Status Status
	// DynamicNodes must be empty if the actual nodes are statically listed in the store
	DynamicNodes []store.Node
}

type Driver interface {
	Factory() Factory
	Bootstrap(context.Context, progress.Logger) error
	Info(context.Context) (*Info, error)
	Stop(ctx context.Context, force bool) error
	Rm(ctx context.Context, force bool) error
	Client(ctx context.Context) (*client.Client, error)
	Features() map[Feature]bool
}

func Boot(ctx context.Context, d Driver, pw progress.Writer) (*client.Client, error) {
	try := 0
	for {
		info, err := d.Info(ctx)
		if err != nil {
			return nil, err
		}
		try++
		if info.Status != Running {
			if try > 2 {
				return nil, errors.Errorf("failed to bootstrap %T driver in attempts", d)
			}
			if err := d.Bootstrap(ctx, func(s *client.SolveStatus) {
				if pw != nil {
					pw.Status() <- s
				}
			}); err != nil {
				return nil, err
			}
		}

		c, err := d.Client(context.TODO())
		if err != nil {
			if errors.Cause(err) == ErrNotRunning && try <= 2 {
				continue
			}
			return nil, err
		}
		return c, nil
	}
}
