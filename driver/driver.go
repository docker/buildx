package driver

import (
	"context"
	"time"

	"github.com/moby/buildkit/client"
	"github.com/pkg/errors"
	"github.com/tonistiigi/buildx/util/progress"
)

var ErrNotRunning = errors.Errorf("driver not running")
var ErrNotConnecting = errors.Errorf("driver not connection")

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
	Bootstrap(context.Context, progress.Logger) error
	Info(context.Context) (*Info, error)
	Stop(ctx context.Context, force bool) error
	Rm(ctx context.Context, force bool) error
	Client(ctx context.Context) (*client.Client, error)
}

func Boot(ctx context.Context, d Driver, pw progress.Writer) (*client.Client, progress.Writer, error) {
	try := 0
	for {
		info, err := d.Info(ctx)
		if err != nil {
			return nil, nil, err
		}
		try++
		if info.Status != Running {
			if try > 2 {
				return nil, nil, errors.Errorf("failed to bootstrap %T driver in attempts", d)
			}
			if err := d.Bootstrap(ctx, func(s *client.SolveStatus) {
				if pw != nil {
					pw.Status() <- s
				}
			}); err != nil {
				return nil, nil, err
			}
		}

		c, err := d.Client(ctx)
		if err != nil {
			if errors.Cause(err) == ErrNotRunning && try <= 2 {
				continue
			}
			return nil, nil, err
		}
		return c, newResetWriter(pw), nil
	}
}

func newResetWriter(in progress.Writer) progress.Writer {
	w := &pw{Writer: in, status: make(chan *client.SolveStatus), tm: time.Now()}
	go func() {
		for {
			select {
			case <-in.Done():
				return
			case st, ok := <-w.status:
				if !ok {
					close(in.Status())
					return
				}
				if w.diff == nil {
					for _, v := range st.Vertexes {
						if v.Started != nil {
							d := v.Started.Sub(w.tm)
							w.diff = &d
						}
					}
				}
				if w.diff != nil {
					for _, v := range st.Vertexes {
						if v.Started != nil {
							d := v.Started.Add(-*w.diff)
							v.Started = &d
						}
						if v.Completed != nil {
							d := v.Completed.Add(-*w.diff)
							v.Completed = &d
						}
					}
					for _, v := range st.Statuses {
						if v.Started != nil {
							d := v.Started.Add(-*w.diff)
							v.Started = &d
						}
						if v.Completed != nil {
							d := v.Completed.Add(-*w.diff)
							v.Completed = &d
						}
						v.Timestamp = v.Timestamp.Add(-*w.diff)
					}
					for _, v := range st.Logs {
						v.Timestamp = v.Timestamp.Add(-*w.diff)
					}
				}
				in.Status() <- st
			}
		}
	}()
	return w
}

type pw struct {
	progress.Writer
	tm     time.Time
	diff   *time.Duration
	status chan *client.SolveStatus
}

func (p *pw) Status() chan *client.SolveStatus {
	return p.status
}
