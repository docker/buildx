package sourcemeta

import (
	"context"
	"errors"
	"slices"
	"sync"
	"sync/atomic"

	"github.com/docker/buildx/util/progress"
	"github.com/moby/buildkit/client"
	"github.com/moby/buildkit/client/llb"
	"github.com/moby/buildkit/client/llb/sourceresolver"
	gwclient "github.com/moby/buildkit/frontend/gateway/client"
	"github.com/moby/buildkit/session"
	"github.com/moby/buildkit/solver/pb"
)

var _ sourceresolver.MetaResolver = &Resolver{}

type gatewayResolver interface {
	sourceresolver.MetaResolver
	Solve(context.Context, gwclient.SolveRequest) (*gwclient.Result, error)
}

type Resolver struct {
	startOnce sync.Once
	closeOnce sync.Once
	openOnce  sync.Once
	started   atomic.Bool
	mu        sync.Mutex

	ready   chan gatewayResolver
	opened  chan struct{}
	done    chan struct{}
	openErr error
	doneErr error
	cancel  context.CancelCauseFunc

	metaResolver gatewayResolver
	run          func(context.Context, chan<- gatewayResolver) error
	closeErr     error
}

type Option func(*newResolverOpts)

type newResolverOpts struct {
	progressWriter progress.Writer
	session        []session.Attachable
}

func WithProgressWriter(pw progress.Writer) Option {
	return func(o *newResolverOpts) {
		o.progressWriter = pw
	}
}

func WithSession(session []session.Attachable) Option {
	return func(o *newResolverOpts) {
		o.session = slices.Clone(session)
	}
}

func NewResolver(c *client.Client, opts ...Option) *Resolver {
	var cfg newResolverOpts
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}

	return newWithRun(func(ctx context.Context, ready chan<- gatewayResolver) error {
		var (
			statusChan chan *client.SolveStatus
			done       chan struct{}
		)
		if cfg.progressWriter != nil {
			statusChan, done = progress.NewChannel(progress.WithPrefix(cfg.progressWriter, "policy", false))
			defer func() {
				<-done
			}()
		}

		solveOpt := client.SolveOpt{
			Internal: true,
		}
		if len(cfg.session) > 0 {
			solveOpt.Session = cfg.session
		}

		_, err := c.Build(ctx, solveOpt, "buildx", func(ctx context.Context, gw gwclient.Client) (*gwclient.Result, error) {
			ready <- gw
			<-ctx.Done()
			return nil, context.Cause(ctx)
		}, statusChan)
		return err
	})
}

func newWithRun(run func(context.Context, chan<- gatewayResolver) error) *Resolver {
	return &Resolver{
		ready:  make(chan gatewayResolver, 1),
		opened: make(chan struct{}),
		done:   make(chan struct{}),
		run:    run,
	}
}

func (r *Resolver) ResolveSourceMetadata(ctx context.Context, op *pb.SourceOp, opt sourceresolver.Opt) (*sourceresolver.MetaResponse, error) {
	mr, err := r.open(ctx)
	if err != nil {
		return nil, err
	}
	return mr.ResolveSourceMetadata(ctx, op, opt)
}

func (r *Resolver) ResolveState(ctx context.Context, st llb.State) (gwclient.Reference, error) {
	mr, err := r.open(ctx)
	if err != nil {
		return nil, err
	}

	def, err := st.Marshal(ctx)
	if err != nil {
		return nil, err
	}

	res, err := mr.Solve(ctx, gwclient.SolveRequest{
		Definition: def.ToPB(),
	})
	if err != nil {
		return nil, err
	}

	ref, err := res.SingleRef()
	if err != nil {
		return nil, err
	}
	return ref, nil
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

func (r *Resolver) open(ctx context.Context) (gatewayResolver, error) {
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
				r.openOnce.Do(func() {
					close(r.opened)
				})
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
				r.openOnce.Do(func() {
					close(r.opened)
				})
			}
			r.mu.Unlock()
		case <-r.opened:
		case <-ctx.Done():
			return nil, context.Cause(ctx)
		}
	}
}
