package build

import (
	"context"
	"testing"
	"time"

	noderesolver "github.com/docker/buildx/build/resolver"
	"github.com/docker/buildx/util/buildflags"
	"github.com/docker/buildx/util/waitmap"
	"github.com/moby/buildkit/client"
	"github.com/moby/buildkit/solver/errdefs"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"
)

func TestLinkedTargetStateIsLinked(t *testing.T) {
	state := newLinkedTargetState(
		map[string][]string{"child": {"parent"}},
		map[string][]string{"parent": {"child"}},
		nil,
		false,
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
	state := newLinkedTargetState(parents, children, nil, false)

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
	state := newLinkedTargetState(parents, children, parents, false)

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
		map[string][]string{"child": {"missing-parent"}},
		false,
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

func TestLinkedTargetStateErrorDirection(t *testing.T) {
	links := func() *linkedTargetState {
		return newLinkedTargetState(
			map[string][]string{"child": {"parent"}},
			map[string][]string{"parent": {"child"}},
			map[string][]string{"child": {"parent"}},
			false,
		)
	}
	t.Run("failed-parent", func(t *testing.T) {
		state := links()
		cause := errors.New("parent failed")
		state.fail("parent", cause)

		err := state.run(t.Context(), "child", struct{}{}, linkedTargetHooks{
			evaluate: func() error { return nil },
		})
		require.ErrorIs(t, err, cause)
	})
	t.Run("failed-child", func(t *testing.T) {
		state := links()
		cause := errors.New("child failed")
		parentDone := make(chan error, 1)
		go func() {
			parentDone <- state.run(t.Context(), "parent", struct{}{}, linkedTargetHooks{
				evaluate: func() error { return nil },
			})
		}()
		err := state.run(t.Context(), "child", struct{}{}, linkedTargetHooks{
			evaluate: func() error { return cause },
		})
		state.fail("child", err)
		require.ErrorIs(t, err, cause)
		require.NoError(t, <-parentDone)
	})
}

func TestLinkedTargetStateEvaluatesAlongsideParent(t *testing.T) {
	state := newLinkedTargetState(
		map[string][]string{"child": {"parent"}},
		map[string][]string{"parent": {"child"}},
		nil,
		false,
	)
	childStarted := make(chan struct{})
	parentDone := make(chan error, 1)
	go func() {
		parentDone <- state.run(t.Context(), "parent", struct{}{}, linkedTargetHooks{
			evaluate: func() error {
				select {
				case <-childStarted:
					return nil
				case <-time.After(5 * time.Second):
					return errors.New("child did not evaluate alongside parent")
				}
			},
		})
	}()
	err := state.run(t.Context(), "child", struct{}{}, linkedTargetHooks{
		evaluate: func() error {
			close(childStarted)
			return nil
		},
	})
	require.NoError(t, err)
	require.NoError(t, <-parentDone)
}

func TestLinkedTargetStateAlongsideParentDeferredErrors(t *testing.T) {
	// run evaluates parent and child concurrently and returns the child's error
	// once both targets are done.
	run := func(t *testing.T, parentErr, childErr error) error {
		state := newLinkedTargetState(
			map[string][]string{"child": {"parent"}},
			map[string][]string{"parent": {"child"}},
			nil,
			true,
		)
		childStarted := make(chan struct{})
		parentDone := make(chan struct{})
		go func() {
			defer close(parentDone)
			err := state.run(t.Context(), "parent", struct{}{}, linkedTargetHooks{
				evaluate: func() error {
					<-childStarted
					return parentErr
				},
			})
			if err != nil {
				state.fail("parent", err)
			}
		}()
		err := state.run(t.Context(), "child", struct{}{}, linkedTargetHooks{
			evaluate: func() error {
				close(childStarted)
				return childErr
			},
		})
		if err != nil {
			state.fail("child", err)
		}
		<-parentDone
		return err
	}

	t.Run("failed-parent", func(t *testing.T) {
		parentErr := errdefs.WrapVertex(errors.New("parent failed"), "sha256:shared")
		err := run(t, parentErr, errdefs.WrapVertex(errors.New("shared vertex failed"), "sha256:shared"))
		var abortErr targetAbortError
		require.ErrorAs(t, err, &abortErr)
		require.ErrorIs(t, err, parentErr)
		require.ErrorContains(t, err, abortedDependencyMsg)
	})
	t.Run("independent-child-failure", func(t *testing.T) {
		childErr := errdefs.WrapVertex(errors.New("child failed"), "sha256:child")
		err := run(t, errdefs.WrapVertex(errors.New("parent failed"), "sha256:parent"), childErr)
		var abortErr targetAbortError
		require.False(t, errors.As(err, &abortErr))
		require.ErrorIs(t, err, childErr)
	})
	t.Run("failed-child", func(t *testing.T) {
		childErr := errors.New("child failed")
		err := run(t, nil, childErr)
		var abortErr targetAbortError
		require.False(t, errors.As(err, &abortErr))
		require.ErrorIs(t, err, childErr)
	})
}

func TestLinkedTargetStateAlongsideParentFailFast(t *testing.T) {
	state := newLinkedTargetState(
		map[string][]string{"child": {"parent"}},
		map[string][]string{"parent": {"child"}},
		nil,
		false,
	)
	childStarted := make(chan struct{})
	releaseParent := make(chan struct{})
	parentDone := make(chan error, 1)
	go func() {
		parentDone <- state.run(t.Context(), "parent", struct{}{}, linkedTargetHooks{
			evaluate: func() error {
				<-childStarted
				<-releaseParent
				return nil
			},
		})
	}()
	// Without deferred errors the child reports its failure while the parent is
	// still evaluating, so the failure can cancel the build.
	childErr := errors.New("child failed")
	childDone := make(chan error, 1)
	go func() {
		childDone <- state.run(t.Context(), "child", struct{}{}, linkedTargetHooks{
			evaluate: func() error {
				close(childStarted)
				return childErr
			},
		})
	}()
	var err error
	select {
	case err = <-childDone:
	case <-time.After(5 * time.Second):
		t.Fatal("child waited for the parent to finish evaluating")
	}
	var abortErr targetAbortError
	require.False(t, errors.As(err, &abortErr))
	require.ErrorIs(t, err, childErr)
	state.fail("child", err)
	close(releaseParent)
	require.NoError(t, <-parentDone)
}

func TestCalculateTargetLinks(t *testing.T) {
	node := &noderesolver.ResolvedNode{}
	key := func(name string) string { return resultKey(node, name) }
	secret := func(id, src string) *buildflags.Secret { return &buildflags.Secret{ID: id, FilePath: src} }

	tcs := []struct {
		name          string
		opts          map[string]Options
		evaluateAfter map[string][]string
	}{
		{
			name: "no-session-attachables",
			opts: map[string]Options{
				"parent": {},
				"child":  {},
			},
			evaluateAfter: map[string][]string{},
		},
		{
			name: "parent-secret-missing-in-child",
			opts: map[string]Options{
				"parent": {SecretSpecs: buildflags.Secrets{secret("token", "a")}},
				"child":  {},
			},
			evaluateAfter: map[string][]string{key("child"): {key("parent")}},
		},
		{
			name: "same-secrets",
			opts: map[string]Options{
				"parent": {SecretSpecs: buildflags.Secrets{secret("token", "a"), secret("other", "b")}},
				"child":  {SecretSpecs: buildflags.Secrets{secret("other", "b"), secret("token", "a")}},
			},
			evaluateAfter: map[string][]string{},
		},
		{
			name: "extra-secret-in-child",
			opts: map[string]Options{
				"parent": {SecretSpecs: buildflags.Secrets{secret("token", "a")}},
				"child":  {SecretSpecs: buildflags.Secrets{secret("other", "b"), secret("token", "a")}},
			},
			evaluateAfter: map[string][]string{key("child"): {key("parent")}},
		},
		{
			name: "parent-secret-with-different-source-in-child",
			opts: map[string]Options{
				"parent": {SecretSpecs: buildflags.Secrets{secret("token", "a")}},
				"child":  {SecretSpecs: buildflags.Secrets{secret("token", "b")}},
			},
			evaluateAfter: map[string][]string{key("child"): {key("parent")}},
		},
		{
			name: "parent-ssh-missing-in-child",
			opts: map[string]Options{
				"parent": {SSHSpecs: []*buildflags.SSH{{ID: "default"}}},
				"child":  {},
			},
			evaluateAfter: map[string][]string{key("child"): {key("parent")}},
		},
		{
			name: "same-ssh",
			opts: map[string]Options{
				"parent": {SSHSpecs: []*buildflags.SSH{{ID: "default"}}},
				"child":  {SSHSpecs: []*buildflags.SSH{{ID: "default"}}},
			},
			evaluateAfter: map[string][]string{},
		},
		{
			name: "extra-ssh-in-child",
			opts: map[string]Options{
				"parent": {},
				"child":  {SSHSpecs: []*buildflags.SSH{{ID: "default"}}},
			},
			evaluateAfter: map[string][]string{key("child"): {key("parent")}},
		},
		{
			name: "parent-stdin-context",
			opts: map[string]Options{
				"parent": {Inputs: Inputs{ContextPath: "-"}},
				"child":  {},
			},
			evaluateAfter: map[string][]string{key("child"): {key("parent")}},
		},
		{
			name: "grandparent-secret-missing-in-child",
			opts: map[string]Options{
				"grandparent": {SecretSpecs: buildflags.Secrets{secret("token", "a")}},
				"parent":      {SecretSpecs: buildflags.Secrets{secret("token", "a")}},
				"child":       {},
			},
			evaluateAfter: map[string][]string{
				key("child"): {key("parent"), key("grandparent")},
			},
		},
		{
			name: "grandparent-secret-in-child",
			opts: map[string]Options{
				"grandparent": {SecretSpecs: buildflags.Secrets{secret("token", "a")}},
				"parent":      {},
				"child":       {SecretSpecs: buildflags.Secrets{secret("token", "a")}},
			},
			evaluateAfter: map[string][]string{
				key("parent"): {key("grandparent")},
				key("child"):  {key("parent")},
			},
		},
	}
	contexts := map[string]string{
		"parent": "grandparent",
		"child":  "parent",
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			reqs := map[string][]*reqForNode{}
			for name := range tc.opts {
				attrs := map[string]string{}
				if parent, ok := contexts[name]; ok {
					if _, ok := tc.opts[parent]; ok {
						attrs["context:base"] = "target:" + parent
					}
				}
				reqs[name] = []*reqForNode{{ResolvedNode: node, so: &client.SolveOpt{FrontendAttrs: attrs}}}
			}
			_, _, evaluateAfter := calculateTargetLinks(reqs, tc.opts)
			require.Equal(t, tc.evaluateAfter, evaluateAfter)
		})
	}
}

func TestSyncEvaluateDoesNotDeadlockLinkedTargets(t *testing.T) {
	linked := newLinkedTargetState(
		map[string][]string{"child": {"parent"}},
		map[string][]string{"parent": {"child"}},
		map[string][]string{"child": {"parent"}},
		false,
	)
	state := &syncTargetState{
		targets:   []string{"parent", "child"},
		results:   waitmap.New(),
		evaluated: waitmap.New(),
	}

	done := map[string]chan error{
		"parent": make(chan error, 1),
		"child":  make(chan error, 1),
	}

	for _, key := range []string{"parent", "child"} {
		go func() {
			done[key] <- linked.run(t.Context(), key, struct{}{}, linkedTargetHooks{
				preEvaluate: func() error {
					return state.waitResult(t.Context(), key, struct{}{})
				},
				evaluate: func() error {
					return nil
				},
				postEvaluate: func() error {
					return state.waitEvaluated(t.Context(), key, struct{}{})
				},
			})
		}()
	}

	require.NoError(t, <-done["parent"])
	require.NoError(t, <-done["child"])
}

func TestSyncEvaluatePropagatesEvaluationErrors(t *testing.T) {
	linked := newLinkedTargetState(map[string][]string{}, map[string][]string{}, nil, false)
	state := &syncTargetState{
		targets:   []string{"success", "failure"},
		results:   waitmap.New(),
		evaluated: waitmap.New(),
	}
	cause := errors.New("target failed")
	done := map[string]chan error{
		"success": make(chan error, 1),
		"failure": make(chan error, 1),
	}

	go func() {
		done["success"] <- linked.run(t.Context(), "success", struct{}{}, linkedTargetHooks{
			evaluate: func() error { return nil },
			postEvaluate: func() error {
				return state.waitEvaluated(t.Context(), "success", struct{}{})
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
		state.fail("failure", err)
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
