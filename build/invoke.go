package build

import (
	"context"
	_ "crypto/sha256" // ensure digests can be computed
	"io"
	"sync"
	"sync/atomic"
	"syscall"

	gateway "github.com/moby/buildkit/frontend/gateway/client"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/tonistiigi/fsutil/types"
)

type InvokeConfig struct {
	Entrypoint []string  `json:"entrypoint,omitempty"`
	Cmd        []string  `json:"cmd,omitempty"`
	NoCmd      bool      `json:"noCmd,omitempty"`
	Env        []string  `json:"env,omitempty"`
	User       string    `json:"user,omitempty"`
	NoUser     bool      `json:"noUser,omitempty"`
	Cwd        string    `json:"cwd,omitempty"`
	NoCwd      bool      `json:"noCwd,omitempty"`
	Tty        bool      `json:"tty,omitempty"`
	Rollback   bool      `json:"rollback,omitempty"`
	Initial    bool      `json:"initial,omitempty"`
	SuspendOn  SuspendOn `json:"suspendOn,omitempty"`
}

func (cfg *InvokeConfig) NeedsDebug(err error) bool {
	return cfg.SuspendOn.DebugEnabled(err)
}

type SuspendOn int

const (
	SuspendError SuspendOn = iota
	SuspendAlways
)

func (s SuspendOn) DebugEnabled(err error) bool {
	return err != nil || s == SuspendAlways
}

func (s *SuspendOn) UnmarshalText(text []byte) error {
	switch string(text) {
	case "error":
		*s = SuspendError
	case "always":
		*s = SuspendAlways
	default:
		return errors.Errorf("unknown suspend name: %s", string(text))
	}
	return nil
}

type Container struct {
	cancelOnce      sync.Once
	containerCancel func(error)
	isUnavailable   atomic.Bool
	initStarted     atomic.Bool
	container       gateway.Container
	releaseCh       chan struct{}
	resultCtx       *ResultHandle
}

func NewContainer(ctx context.Context, resultCtx *ResultHandle, cfg *InvokeConfig) (*Container, error) {
	mainCtx := ctx

	ctrCh := make(chan *Container, 1)
	errCh := make(chan error, 1)
	go func() {
		err := func() error {
			containerCtx, containerCancel := context.WithCancelCause(ctx)
			defer containerCancel(errors.WithStack(context.Canceled))

			bkContainer, err := resultCtx.NewContainer(containerCtx, cfg)
			if err != nil {
				return err
			}

			releaseCh := make(chan struct{})
			container := &Container{
				containerCancel: containerCancel,
				container:       bkContainer,
				releaseCh:       releaseCh,
				resultCtx:       resultCtx,
			}
			doneCh := make(chan struct{})
			defer close(doneCh)
			resultCtx.registerCleanup(func() {
				container.Cancel()
				<-doneCh
			})
			ctrCh <- container
			<-container.releaseCh

			return bkContainer.Release(ctx)
		}()
		if err != nil {
			errCh <- err
		}
	}()
	select {
	case ctr := <-ctrCh:
		return ctr, nil
	case err := <-errCh:
		return nil, err
	case <-mainCtx.Done():
		return nil, mainCtx.Err()
	}
}

func (c *Container) Cancel() {
	c.markUnavailable()
	c.cancelOnce.Do(func() {
		if c.containerCancel != nil {
			c.containerCancel(errors.WithStack(context.Canceled))
		}
		close(c.releaseCh)
	})
}

func (c *Container) IsUnavailable() bool {
	return c.isUnavailable.Load()
}

func (c *Container) markUnavailable() {
	c.isUnavailable.Store(true)
}

func (c *Container) Exec(ctx context.Context, cfg *InvokeConfig, stdin io.ReadCloser, stdout io.WriteCloser, stderr io.WriteCloser) error {
	if isInit := c.initStarted.CompareAndSwap(false, true); isInit {
		defer func() {
			// container can't be used after init exits
			c.markUnavailable()
		}()
	}
	err := exec(ctx, c.resultCtx, cfg, c.container, stdin, stdout, stderr)
	if err != nil {
		// Container becomes unavailable if one of the processes fails in it.
		c.markUnavailable()
	}
	return err
}

func (c *Container) ReadFile(ctx context.Context, req gateway.ReadContainerRequest) ([]byte, error) {
	return c.container.ReadFile(ctx, req)
}

func (c *Container) ReadDir(ctx context.Context, req gateway.ReadDirContainerRequest) ([]*types.Stat, error) {
	return c.container.ReadDir(ctx, req)
}

func (c *Container) StatFile(ctx context.Context, req gateway.StatContainerRequest) (*types.Stat, error) {
	return c.container.StatFile(ctx, req)
}

func exec(ctx context.Context, resultCtx *ResultHandle, cfg *InvokeConfig, ctr gateway.Container, stdin io.ReadCloser, stdout io.WriteCloser, stderr io.WriteCloser) error {
	processCfg, err := resultCtx.getProcessConfig(cfg, stdin, stdout, stderr)
	if err != nil {
		return err
	}
	proc, err := ctr.Start(ctx, processCfg)
	if err != nil {
		return errors.Errorf("failed to start container: %v", err)
	}

	doneCh := make(chan struct{})
	defer close(doneCh)
	go func() {
		select {
		case <-ctx.Done():
			if err := proc.Signal(ctx, syscall.SIGKILL); err != nil {
				logrus.Warnf("failed to kill process: %v", err)
			}
		case <-doneCh:
		}
	}()

	return proc.Wait()
}
