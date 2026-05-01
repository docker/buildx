package build_test

import (
	"context"
	"testing"

	"github.com/docker/buildx/build"
	gateway "github.com/moby/buildkit/frontend/gateway/client"
	"github.com/moby/buildkit/solver/errdefs"
	"github.com/moby/buildkit/solver/pb"
	"github.com/stretchr/testify/require"
)

func Test_Creating_new_container_from_failing_result_handle(t *testing.T) {
	t.Parallel()

	mounts := []*pb.Mount{
		{Dest: "/", Input: 0},
		{Dest: "/src", Input: 1},
	}

	run := func(t *testing.T, cfg build.InvokeConfig, inputIDs, mountIDs []string) (*gateway.NewContainerRequest, error) {
		t.Helper()
		ctx := t.Context()
		gw := &stubGwClient{}
		solveErr := &errdefs.SolveError{
			Solve: &errdefs.Solve{
				Op:       &pb.Op{Op: &pb.Op_Exec{Exec: &pb.ExecOp{Mounts: mounts}}},
				InputIDs: inputIDs,
				MountIDs: mountIDs,
			},
		}
		rh := build.NewResultHandle(ctx, gw, nil, nil, solveErr)
		require.NotNil(t, rh, "NewResultHandle must accept a *errdefs.SolveError")
		defer rh.Done()

		var err error
		require.NotPanics(t, func() {
			_, err = rh.NewContainer(ctx, &cfg)
		}, "ResultHandle.NewContainer must not panic")
		return gw.captured, err
	}

	t.Run("returns_an_error_when", func(t *testing.T) {
		t.Parallel()

		cases := []struct {
			name     string
			inputIDs []string
			mountIDs []string
			cfg      build.InvokeConfig
		}{
			{
				name:     "mount_ids_are_missing",
				mountIDs: nil,
				cfg:      build.InvokeConfig{},
			},
			{
				name:     "input_ids_are_missing_and_initial_is_true",
				inputIDs: nil,
				cfg:      build.InvokeConfig{Initial: true},
			},
			{
				name:     "mount_ids_are_shorter_than_declared_mounts",
				inputIDs: []string{"input-0"},
				mountIDs: []string{"mount-0"},
				cfg:      build.InvokeConfig{},
			},
			{
				name:     "input_ids_are_shorter_than_declared_mounts_and_initial_is_true",
				inputIDs: []string{"input-0"},
				mountIDs: []string{"mount-0"},
				cfg:      build.InvokeConfig{Initial: true},
			},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()
				_, err := run(t, tc.cfg, tc.inputIDs, tc.mountIDs)
				require.Error(t, err)
			})
		}
	})

	t.Run("forwards_per_mount_result_ids_to_the_gateway", func(t *testing.T) {
		t.Parallel()

		cases := []struct {
			name          string
			cfg           build.InvokeConfig
			wantResultIDs []string
		}{
			{
				name:          "from_mount_ids",
				cfg:           build.InvokeConfig{},
				wantResultIDs: []string{"mount-0", "mount-1"},
			},
			{
				name:          "from_input_ids_when_initial_is_true",
				cfg:           build.InvokeConfig{Initial: true},
				wantResultIDs: []string{"input-0", "input-1"},
			},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()
				captured, err := run(t, tc.cfg,
					[]string{"input-0", "input-1"},
					[]string{"mount-0", "mount-1"},
				)
				require.NoError(t, err)
				require.NotNil(t, captured, "gateway.NewContainer should have been called")
				require.Len(t, captured.Mounts, len(tc.wantResultIDs))
				for i, want := range tc.wantResultIDs {
					require.Equal(t, want, captured.Mounts[i].ResultID, "mount %d ResultID", i)
					require.Equal(t, mounts[i].Dest, captured.Mounts[i].Dest, "mount %d Dest", i)
				}
			})
		}
	})
}

type stubGwClient struct {
	gateway.Client
	captured *gateway.NewContainerRequest
}

func (s *stubGwClient) NewContainer(_ context.Context, req gateway.NewContainerRequest) (gateway.Container, error) {
	s.captured = &req

	return nil, nil
}
