package build

import (
	"context"
	"testing"
	"time"

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
		done["root"] <- state.run(t.Context(), "root", struct{}{}, func() error { return nil })
	}()
	go func() {
		done["middle"] <- state.run(t.Context(), "middle", struct{}{}, func() error { return nil })
	}()
	go func() {
		done["leaf"] <- state.run(t.Context(), "leaf", struct{}{}, func() error {
			close(leafStarted)
			<-releaseLeaf
			return nil
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
			done <- state.run(t.Context(), key, struct{}{}, evaluate)
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
		done <- state.run(ctx, "child", struct{}{}, func() error { return nil })
	}()
	cancel(cause)
	require.ErrorIs(t, <-done, cause)
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
