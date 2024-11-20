package build

import (
	"context"
	_ "crypto/sha256" // ensure digests can be computed
	"io"
	"sync"
	"sync/atomic"
	"syscall"

	controllerapi "github.com/docker/buildx/controller/pb"
	gateway "github.com/moby/buildkit/frontend/gateway/client"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

type Container struct {
	cancelOnce      sync.Once
	containerCancel func(error)
	isUnavailable   atomic.Bool
	initStarted     atomic.Bool
	container       gateway.Container
	releaseCh       chan struct{}
	resultCtx       *ResultHandle
}

func NewContainer(ctx context.Context, resultCtx *ResultHandle, cfg *controllerapi.InvokeConfig) (*Container, error) {
	mainCtx := ctx

	ctrCh := make(chan *Container)
	errCh := make(chan error)
	go func() {
		err := resultCtx.build(func(ctx context.Context, c gateway.Client) (*gateway.Result, error) {
			ctx, cancel := context.WithCancelCause(ctx)
			go func() {
				<-mainCtx.Done()
				cancel(errors.WithStack(context.Canceled))
			}()

			containerCfg, err := resultCtx.getContainerConfig(cfg)
			if err != nil {
				return nil, err
			}
			containerCtx, containerCancel := context.WithCancelCause(ctx)
			defer containerCancel(errors.WithStack(context.Canceled))
			bkContainer, err := c.NewContainer(containerCtx, containerCfg)
			if err != nil {
				return nil, err
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

			return nil, bkContainer.Release(ctx)
		})
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

func (c *Container) Exec(ctx context.Context, cfg *controllerapi.InvokeConfig, stdin io.ReadCloser, stdout io.WriteCloser, stderr io.WriteCloser) error {
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

func exec(ctx context.Context, resultCtx *ResultHandle, cfg *controllerapi.InvokeConfig, ctr gateway.Container, stdin io.ReadCloser, stdout io.WriteCloser, stderr io.WriteCloser) error {
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
