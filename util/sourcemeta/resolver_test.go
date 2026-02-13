package sourcemeta

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/moby/buildkit/client/llb/sourceresolver"
	"github.com/moby/buildkit/solver/pb"
	"github.com/stretchr/testify/require"
)

type fakeMetaResolver struct {
	calls atomic.Int32
	resp  *sourceresolver.MetaResponse
	err   error
}

func (f *fakeMetaResolver) ResolveSourceMetadata(ctx context.Context, op *pb.SourceOp, opt sourceresolver.Opt) (*sourceresolver.MetaResponse, error) {
	f.calls.Add(1)
	return f.resp, f.err
}

func TestResolverCloseNoopBeforeResolve(t *testing.T) {
	t.Parallel()

	var called atomic.Int32
	r := newWithRun(func(ctx context.Context, ready chan<- sourceresolver.MetaResolver) error {
		called.Add(1)
		return nil
	})

	require.NoError(t, r.Close())
	require.EqualValues(t, 0, called.Load())
}

func TestResolverResolveOpensOnce(t *testing.T) {
	t.Parallel()

	var runs atomic.Int32
	mr := &fakeMetaResolver{resp: &sourceresolver.MetaResponse{}}
	r := newWithRun(func(ctx context.Context, ready chan<- sourceresolver.MetaResolver) error {
		runs.Add(1)
		ready <- mr
		<-ctx.Done()
		return context.Cause(ctx)
	})

	op := &pb.SourceOp{}
	_, err := r.ResolveSourceMetadata(t.Context(), op, sourceresolver.Opt{})
	require.NoError(t, err)
	_, err = r.ResolveSourceMetadata(t.Context(), op, sourceresolver.Opt{})
	require.NoError(t, err)

	require.EqualValues(t, 1, runs.Load())
	require.EqualValues(t, 2, mr.calls.Load())
	require.NoError(t, r.Close())
}

func TestResolverCloseAfterOpenCancelsBuild(t *testing.T) {
	t.Parallel()

	var canceled atomic.Bool
	r := newWithRun(func(ctx context.Context, ready chan<- sourceresolver.MetaResolver) error {
		ready <- &fakeMetaResolver{resp: &sourceresolver.MetaResponse{}}
		<-ctx.Done()
		canceled.Store(true)
		return context.Cause(ctx)
	})

	_, err := r.ResolveSourceMetadata(t.Context(), &pb.SourceOp{}, sourceresolver.Opt{})
	require.NoError(t, err)
	require.NoError(t, r.Close())
	require.True(t, canceled.Load())
}

func TestResolverOpenFailureIsSticky(t *testing.T) {
	t.Parallel()

	expected := errors.New("boom")
	var runs atomic.Int32
	r := newWithRun(func(ctx context.Context, ready chan<- sourceresolver.MetaResolver) error {
		runs.Add(1)
		return expected
	})

	_, err := r.ResolveSourceMetadata(t.Context(), &pb.SourceOp{}, sourceresolver.Opt{})
	require.ErrorIs(t, err, expected)
	_, err = r.ResolveSourceMetadata(t.Context(), &pb.SourceOp{}, sourceresolver.Opt{})
	require.ErrorIs(t, err, expected)
	require.EqualValues(t, 1, runs.Load())
	require.ErrorIs(t, r.Close(), expected)
}

func TestResolverCloseIgnoresTerminalContextErrors(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		err  error
	}{
		{name: "canceled", err: context.Canceled},
		{name: "deadline", err: context.DeadlineExceeded},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			r := newWithRun(func(ctx context.Context, ready chan<- sourceresolver.MetaResolver) error {
				return tc.err
			})
			_, err := r.ResolveSourceMetadata(t.Context(), &pb.SourceOp{}, sourceresolver.Opt{})
			require.ErrorIs(t, err, tc.err)
			require.NoError(t, r.Close())
		})
	}
}

func TestResolverConcurrentResolveUsesSingleOpen(t *testing.T) {
	t.Parallel()

	var runs atomic.Int32
	mr := &fakeMetaResolver{resp: &sourceresolver.MetaResponse{}}
	r := newWithRun(func(ctx context.Context, ready chan<- sourceresolver.MetaResolver) error {
		runs.Add(1)
		ready <- mr
		<-ctx.Done()
		return context.Cause(ctx)
	})

	const n = 16
	errCh := make(chan error, n)
	var wg sync.WaitGroup
	wg.Add(n)
	for range n {
		go func() {
			defer wg.Done()
			_, err := r.ResolveSourceMetadata(t.Context(), &pb.SourceOp{}, sourceresolver.Opt{})
			errCh <- err
		}()
	}
	wg.Wait()
	close(errCh)

	for err := range errCh {
		require.NoError(t, err)
	}
	require.EqualValues(t, 1, runs.Load())
	require.EqualValues(t, n, mr.calls.Load())

	done := make(chan struct{})
	closeErr := make(chan error, 1)
	go func() {
		defer close(done)
		closeErr <- r.Close()
	}()
	select {
	case <-done:
		require.NoError(t, <-closeErr)
	case <-time.After(2 * time.Second):
		t.Fatal("close timed out")
	}
}

func TestResolverFirstCanceledContextDoesNotPoisonFutureCalls(t *testing.T) {
	t.Parallel()

	mr := &fakeMetaResolver{resp: &sourceresolver.MetaResponse{}}
	started := make(chan struct{})
	release := make(chan struct{})

	r := newWithRun(func(ctx context.Context, ready chan<- sourceresolver.MetaResolver) error {
		close(started)
		<-release
		ready <- mr
		<-ctx.Done()
		return context.Cause(ctx)
	})

	canceledCtx, cancel := context.WithCancelCause(t.Context())
	cancel(context.Canceled)
	_, err := r.ResolveSourceMetadata(canceledCtx, &pb.SourceOp{}, sourceresolver.Opt{})
	require.ErrorIs(t, err, context.Canceled)

	<-started
	close(release)

	_, err = r.ResolveSourceMetadata(t.Context(), &pb.SourceOp{}, sourceresolver.Opt{})
	require.NoError(t, err)
	require.NoError(t, r.Close())
}
