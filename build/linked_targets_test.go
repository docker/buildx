package build

import (
	"context"
	"testing"
	"time"

	"github.com/docker/buildx/util/waitmap"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"
)

func TestLinkedTargetStateIsLinked(t *testing.T) {
	state := newLinkedTargetState(
		map[string][]string{"child": {"parent"}},
		map[string][]string{"parent": {"child"}},
	)
	require.True(t, state.isLinked("parent"))
	require.True(t, state.isLinked("child"))
	require.False(t, state.isLinked("standalone"))
}

func TestLinkedTargetStateChainRetainsParents(t *testing.T) {
	parents := map[string][]string{
		"middle": {"root"},
		"leaf":   {"middle"},
	}
	children := map[string][]string{
		"root":   {"middle"},
		"middle": {"leaf"},
	}
	state := newLinkedTargetState(parents, children)

	leafStarted := make(chan struct{})
	releaseLeaf := make(chan struct{})
	done := map[string]chan error{
		"root":   make(chan error, 1),
		"middle": make(chan error, 1),
		"leaf":   make(chan error, 1),
	}

	go func() {
		done["root"] <- state.run(t.Context(), "root", struct{}{}, linkedTargetHooks{
			evaluate: func() error { return nil },
		})
	}()
	go func() {
		done["middle"] <- state.run(t.Context(), "middle", struct{}{}, linkedTargetHooks{
			evaluate: func() error { return nil },
		})
	}()
	go func() {
		done["leaf"] <- state.run(t.Context(), "leaf", struct{}{}, linkedTargetHooks{
			evaluate: func() error {
				close(leafStarted)
				<-releaseLeaf
				return nil
			},
		})
	}()

	select {
	case <-leafStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("leaf evaluation did not start")
	}
	assertNotCompleted(t, done["root"])
	assertNotCompleted(t, done["middle"])
	close(releaseLeaf)
	require.NoError(t, <-done["leaf"])
	require.NoError(t, <-done["middle"])
	require.NoError(t, <-done["root"])
}

func TestLinkedTargetStateDiamondEvaluatesBranchesInParallel(t *testing.T) {
	parents := map[string][]string{
		"left":  {"root"},
		"right": {"root"},
		"leaf":  {"left", "right"},
	}
	children := map[string][]string{
		"root":  {"left", "right"},
		"left":  {"leaf"},
		"right": {"leaf"},
	}
	state := newLinkedTargetState(parents, children)

	branchesStarted := make(chan string, 2)
	releaseBranches := make(chan struct{})
	done := make(chan error, 4)
	run := func(key string, evaluate func() error) {
		go func() {
			done <- state.run(t.Context(), key, struct{}{}, linkedTargetHooks{
				evaluate: evaluate,
			})
		}()
	}
	run("root", func() error { return nil })
	for _, key := range []string{"left", "right"} {
		run(key, func() error {
			branchesStarted <- key
			<-releaseBranches
			return nil
		})
	}
	leafStarted := make(chan struct{})
	run("leaf", func() error {
		close(leafStarted)
		return nil
	})

	started := map[string]bool{}
	for range 2 {
		select {
		case key := <-branchesStarted:
			started[key] = true
		case <-time.After(5 * time.Second):
			t.Fatal("linked branches did not evaluate in parallel")
		}
	}
	require.Equal(t, map[string]bool{"left": true, "right": true}, started)
	assertNotSignaled(t, leafStarted)
	close(releaseBranches)
	for range 4 {
		require.NoError(t, <-done)
	}
}

func TestLinkedTargetStateCancellation(t *testing.T) {
	state := newLinkedTargetState(
		map[string][]string{"child": {"missing-parent"}},
		map[string][]string{},
	)
	ctx, cancel := context.WithCancelCause(t.Context())
	cause := errors.New("target failed")
	done := make(chan error, 1)
	go func() {
		done <- state.run(ctx, "child", struct{}{}, linkedTargetHooks{
			evaluate: func() error { return nil },
		})
	}()
	cancel(cause)
	require.ErrorIs(t, <-done, cause)
}

func TestLinkedTargetStatePropagatesDependencyErrors(t *testing.T) {
	state := newLinkedTargetState(
		map[string][]string{"child": {"parent"}},
		map[string][]string{"parent": {"child"}},
	)
	cause := errors.New("parent failed")
	state.fail("parent", cause)

	err := state.run(t.Context(), "child", struct{}{}, linkedTargetHooks{
		evaluate: func() error { return nil },
	})
	require.ErrorIs(t, err, cause)
}

func TestSyncEvaluateWaitsForAllTargets(t *testing.T) {
	targets := []string{"foo", "bar"}
	results := waitmap.New()

	fooStarted := make(chan struct{})
	done := map[string]chan error{
		"foo": make(chan error, 1),
		"bar": make(chan error, 1),
	}

	go func() {
		results.Set("foo", struct{}{})
		if _, err := results.Get(t.Context(), targets...); err != nil {
			done["foo"] <- err
			return
		}
		close(fooStarted)
		done["foo"] <- nil
	}()

	assertNotSignaled(t, fooStarted)
	assertNotCompleted(t, done["foo"])

	go func() {
		results.Set("bar", struct{}{})
		if _, err := results.Get(t.Context(), targets...); err != nil {
			done["bar"] <- err
			return
		}
		done["bar"] <- nil
	}()

	require.NoError(t, <-done["foo"])
	require.NoError(t, <-done["bar"])
}

func TestSyncEvaluateCancellation(t *testing.T) {
	results := waitmap.New()
	ctx, cancel := context.WithCancelCause(t.Context())
	cause := errors.New("target failed")

	done := make(chan error, 1)
	go func() {
		results.Set("foo", struct{}{})
		_, err := results.Get(ctx, "foo", "bar")
		done <- err
	}()

	cancel(cause)
	require.ErrorIs(t, <-done, cause)
}

func TestSyncEvaluateDoesNotDeadlockLinkedTargets(t *testing.T) {
	linked := newLinkedTargetState(
		map[string][]string{"child": {"parent"}},
		map[string][]string{"parent": {"child"}},
	)
	results := waitmap.New()
	evaluated := waitmap.New()

	done := map[string]chan error{
		"parent": make(chan error, 1),
		"child":  make(chan error, 1),
	}

	for _, key := range []string{"parent", "child"} {
		go func() {
			done[key] <- linked.run(t.Context(), key, struct{}{}, linkedTargetHooks{
				preEvaluate: func() error {
					results.Set(key, struct{}{})
					_, err := results.Get(t.Context(), "parent", "child")
					return err
				},
				evaluate: func() error {
					return nil
				},
				postEvaluate: func() error {
					evaluated.Set(key, struct{}{})
					_, err := evaluated.Get(t.Context(), "parent", "child")
					return err
				},
			})
		}()
	}

	require.NoError(t, <-done["parent"])
	require.NoError(t, <-done["child"])
}

func TestSyncEvaluatePropagatesEvaluationErrors(t *testing.T) {
	linked := newLinkedTargetState(map[string][]string{}, map[string][]string{})
	evaluated := waitmap.New()
	cause := errors.New("target failed")
	done := map[string]chan error{
		"success": make(chan error, 1),
		"failure": make(chan error, 1),
	}

	go func() {
		done["success"] <- linked.run(t.Context(), "success", struct{}{}, linkedTargetHooks{
			evaluate: func() error {
				return nil
			},
			postEvaluate: func() error {
				evaluated.Set("success", struct{}{})
				results, err := evaluated.Get(t.Context(), "success", "failure")
				if err != nil {
					return err
				}
				return wrapResultError(results, "aborted: another target failed")
			},
		})
	}()

	assertNotCompleted(t, done["success"])

	go func() {
		err := linked.run(t.Context(), "failure", struct{}{}, linkedTargetHooks{
			evaluate: func() error {
				return cause
			},
		})
		evaluated.Set("failure", err)
		done["failure"] <- err
	}()

	err := <-done["success"]
	require.ErrorContains(t, err, "aborted: another target failed")
	require.ErrorIs(t, err, cause)
	require.ErrorIs(t, <-done["failure"], cause)
}

func TestResultErrorReturnsFirstErrorInKeyOrder(t *testing.T) {
	alpha := errors.New("alpha failed")
	beta := errors.New("beta failed")

	err := resultError(map[string]any{
		"b":  beta,
		"ok": struct{}{},
		"a":  alpha,
	})

	require.EqualError(t, err, "alpha failed")
	require.ErrorIs(t, err, alpha)
}

func TestSyncTargetStateWrapsPropagatedErrors(t *testing.T) {
	state := &syncTargetState{
		targets:   []string{"success", "failure"},
		results:   waitmap.New(),
		evaluated: waitmap.New(),
	}
	cause := errors.New("target failed")
	done := make(chan error, 1)

	go func() {
		done <- state.waitResult(t.Context(), "success", struct{}{})
	}()

	assertNotCompleted(t, done)
	state.fail("failure", cause)

	err := <-done
	require.ErrorContains(t, err, "aborted: another target failed")
	require.ErrorIs(t, err, cause)
}

func TestWrapResultErrorDoesNotNestAbortErrors(t *testing.T) {
	cause := errors.New("target failed")
	first := wrapResultError(map[string]any{"root": cause}, "aborted: dependency target failed")
	second := wrapResultError(map[string]any{"mid": first}, "aborted: dependency target failed")

	require.EqualError(t, second, "aborted: dependency target failed: target failed")
	require.ErrorIs(t, second, cause)
}

func assertNotCompleted(t *testing.T, ch <-chan error) {
	t.Helper()
	select {
	case err := <-ch:
		require.Failf(t, "target completed early", "unexpected error: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
}

func assertNotSignaled(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
		require.Fail(t, "target evaluated early")
	case <-time.After(100 * time.Millisecond):
	}
}
