package build

import (
	"context"
	"testing"

	"github.com/docker/buildx/util/waitmap"
	"github.com/moby/buildkit/client"
	"github.com/moby/buildkit/client/llb"
	gateway "github.com/moby/buildkit/frontend/gateway/client"
	"github.com/stretchr/testify/require"
	fstypes "github.com/tonistiigi/fsutil/types"
)

type mockReference struct{}

func (m *mockReference) ToState() (llb.State, error)        { return llb.Scratch(), nil }
func (m *mockReference) Evaluate(ctx context.Context) error { return nil }
func (m *mockReference) ReadFile(ctx context.Context, req gateway.ReadRequest) ([]byte, error) {
	return nil, nil
}
func (m *mockReference) StatFile(ctx context.Context, req gateway.StatRequest) (*fstypes.Stat, error) {
	return nil, nil
}
func (m *mockReference) ReadDir(ctx context.Context, req gateway.ReadDirRequest) ([]*fstypes.Stat, error) {
	return nil, nil
}

// TestWaitContextDepsWithNilRefs reproduces issue #3508 where nil refs in multi-platform
// builds (e.g., FROM scratch) caused a segmentation fault.
func TestWaitContextDepsWithNilRefs(t *testing.T) {
	ctx := context.Background()
	results := waitmap.New()

	result := &gateway.Result{
		Refs: map[string]gateway.Reference{
			"linux/amd64": &mockReference{},
			"linux/arm64": nil, // Nil ref should not panic
		},
	}
	results.Set("0-base", result)

	so := &client.SolveOpt{
		FrontendAttrs: map[string]string{
			"context:base": "target:base",
		},
	}

	err := waitContextDeps(ctx, 0, results, so)
	require.NoError(t, err)

	// Only non-nil platform should be set
	require.Contains(t, so.FrontendAttrs, "context:base::linux/amd64")
	require.NotContains(t, so.FrontendAttrs, "context:base::linux/arm64")
}

// TestWaitContextDepsNormal verifies normal multi-platform operation.
func TestWaitContextDepsNormal(t *testing.T) {
	ctx := context.Background()
	results := waitmap.New()

	result := &gateway.Result{
		Refs: map[string]gateway.Reference{
			"linux/amd64": &mockReference{},
			"linux/arm64": &mockReference{},
		},
	}
	results.Set("0-base", result)

	so := &client.SolveOpt{
		FrontendAttrs: map[string]string{
			"context:base": "target:base",
		},
	}

	err := waitContextDeps(ctx, 0, results, so)
	require.NoError(t, err)
	require.Contains(t, so.FrontendAttrs, "context:base::linux/amd64")
	require.Contains(t, so.FrontendAttrs, "context:base::linux/arm64")
}
