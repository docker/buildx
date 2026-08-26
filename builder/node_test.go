package builder

import (
	"encoding/json"
	"testing"

	"github.com/containerd/platforms"
	"github.com/docker/buildx/store"
	ocispecs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
)

func TestNodeMarshalJSONPlatforms(t *testing.T) {
	t.Parallel()

	n := Node{
		Node: store.Node{Name: "n0", Endpoint: "docker-container://buildx_buildkit_n0"},
		Platforms: []ocispecs.Platform{
			platforms.MustParse("windows(10.0.17763)/amd64"),
			platforms.MustParse("windows(10.0.20348)/amd64"),
			platforms.MustParse("windows(10.0.20348+win32k)/amd64"),
			platforms.MustParse("linux/x86_64"),
		},
	}

	dt, err := json.Marshal(&n)
	require.NoError(t, err)

	var out struct {
		Platforms []string
	}
	require.NoError(t, json.Unmarshal(dt, &out))
	require.Equal(t, []string{
		"windows(10.0.17763)/amd64",
		"windows(10.0.20348)/amd64",
		"windows(10.0.20348+win32k)/amd64",
		"linux/amd64",
	}, out.Platforms)
}

func TestNodeMarshalJSONNoPlatforms(t *testing.T) {
	t.Parallel()

	dt, err := json.Marshal(&Node{Node: store.Node{Name: "n0"}})
	require.NoError(t, err)
	require.NotContains(t, string(dt), "Platforms")
}
