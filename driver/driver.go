package driver

import (
	"context"

	"github.com/moby/buildkit/client"
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
	Status Status
}

type Driver interface {
	Bootstrap(context.Context, Logger) error
	Info(context.Context) (*Info, error)
	Stop(ctx context.Context, force bool) error
	Rm(ctx context.Context, force bool) error
	Client(ctx context.Context) (*client.Client, error)
}

func Boot(ctx context.Context, d Driver, status chan *client.SolveStatus) (*client.Client, error) {
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
				if status != nil {
					status <- s
				}
			}); err != nil {
				return nil, err
			}
		}

		c, err := d.Client(ctx)
		if err != nil {
			if errors.Cause(err) == ErrNotRunning && try <= 2 {
				continue
			}
			return nil, err
		}
		return c, nil
	}

	return nil, errors.Errorf("boot not implemented")
}
