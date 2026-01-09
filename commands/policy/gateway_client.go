package policy

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"

	"github.com/moby/buildkit/client"
	gwclient "github.com/moby/buildkit/frontend/gateway/client"
)

type gatewayClientOpener func(context.Context) (gwclient.Client, error)

func gatewayClientFactory(c *client.Client) (gatewayClientOpener, func() error, error) {
	var (
		once        sync.Once
		releaseOnce sync.Once
		started     atomic.Bool
		ready       = make(chan gwclient.Client, 1)
		done        = make(chan error, 1)
		openErr     error
		releaseErr  error
		gwClient    gwclient.Client
		cancel      context.CancelCauseFunc
	)

	open := func(ctx context.Context) (gwclient.Client, error) {
		once.Do(func() {
			started.Store(true)
			buildCtx, cancelFn := context.WithCancelCause(ctx)
			cancel = cancelFn

			go func() {
				_, err := c.Build(buildCtx, client.SolveOpt{Internal: true}, "buildx", func(ctx context.Context, c gwclient.Client) (*gwclient.Result, error) {
					ready <- c
					<-buildCtx.Done()
					return nil, context.Cause(buildCtx)
				}, nil)
				done <- err
			}()

			select {
			case gwClient = <-ready:
			case err := <-done:
				if err == nil {
					err = errors.New("gateway build finished without a client")
				}
				openErr = err
			case <-ctx.Done():
				openErr = context.Cause(ctx)
				cancelFn(openErr)
			}
		})

		if openErr != nil {
			return nil, openErr
		}
		return gwClient, nil
	}

	release := func() error {
		releaseOnce.Do(func() {
			if !started.Load() {
				return
			}
			if cancel != nil {
				cancel(context.Canceled)
			}
			err := <-done
			if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return
			}
			releaseErr = err
		})
		return releaseErr
	}

	return open, release, nil
}
