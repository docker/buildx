package sourcemeta

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"

	"github.com/moby/buildkit/client"
	"github.com/moby/buildkit/client/llb/sourceresolver"
	gwclient "github.com/moby/buildkit/frontend/gateway/client"
	"github.com/moby/buildkit/solver/pb"
)

var _ sourceresolver.MetaResolver = &Resolver{}

type Resolver struct {
	startOnce sync.Once
	closeOnce sync.Once
	started   atomic.Bool
	mu        sync.Mutex

	ready   chan sourceresolver.MetaResolver
	done    chan struct{}
	openErr error
	doneErr error
	cancel  context.CancelCauseFunc

	metaResolver sourceresolver.MetaResolver
	run          func(context.Context, chan<- sourceresolver.MetaResolver) error
	closeErr     error
}

func NewResolver(c *client.Client) *Resolver {
	return newWithRun(func(ctx context.Context, ready chan<- sourceresolver.MetaResolver) error {
		_, err := c.Build(ctx, client.SolveOpt{Internal: true}, "buildx", func(ctx context.Context, gw gwclient.Client) (*gwclient.Result, error) {
			ready <- gw
			<-ctx.Done()
			return nil, context.Cause(ctx)
		}, nil)
		return err
	})
}

func newWithRun(run func(context.Context, chan<- sourceresolver.MetaResolver) error) *Resolver {
	return &Resolver{
		ready: make(chan sourceresolver.MetaResolver, 1),
		done:  make(chan struct{}),
		run:   run,
	}
}

func (r *Resolver) ResolveSourceMetadata(ctx context.Context, op *pb.SourceOp, opt sourceresolver.Opt) (*sourceresolver.MetaResponse, error) {
	mr, err := r.open(ctx)
	if err != nil {
		return nil, err
	}
	return mr.ResolveSourceMetadata(ctx, op, opt)
}

func (r *Resolver) Close() error {
	r.closeOnce.Do(func() {
		if !r.started.Load() {
			return
		}
		if r.cancel != nil {
			r.cancel(context.Canceled)
		}
		<-r.done
		err := r.doneErr
		if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return
		}
		r.closeErr = err
	})
	return r.closeErr
}

func (r *Resolver) open(ctx context.Context) (sourceresolver.MetaResolver, error) {
	r.startOnce.Do(func() {
		r.started.Store(true)
		buildCtx, cancel := context.WithCancelCause(context.Background())
		r.cancel = cancel

		go func() {
			r.doneErr = r.run(buildCtx, r.ready)
			close(r.done)
		}()
	})

	for {
		r.mu.Lock()
		if r.metaResolver != nil {
			mr := r.metaResolver
			r.mu.Unlock()
			return mr, nil
		}
		if r.openErr != nil {
			err := r.openErr
			r.mu.Unlock()
			return nil, err
		}
		r.mu.Unlock()

		select {
		case mr := <-r.ready:
			r.mu.Lock()
			if r.metaResolver == nil {
				r.metaResolver = mr
			}
			r.mu.Unlock()
		case <-r.done:
			r.mu.Lock()
			if r.metaResolver == nil && r.openErr == nil {
				err := r.doneErr
				if err == nil {
					err = errors.New("gateway build finished without a source metadata resolver")
				}
				r.openErr = err
			}
			r.mu.Unlock()
		case <-ctx.Done():
			return nil, context.Cause(ctx)
		}
	}
}
