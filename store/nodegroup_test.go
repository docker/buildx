package store

import (
	"testing"

	"github.com/docker/buildx/util/platformutil"
	"github.com/stretchr/testify/require"
)

func TestNodeGroupUpdate(t *testing.T) {
	t.Parallel()

	ng := &NodeGroup{}
	err := ng.Update("foo", "foo0", []string{"linux/amd64"}, true, false, []string{"--debug"}, "", nil)
	require.NoError(t, err)

	err = ng.Update("foo1", "foo1", []string{"linux/arm64", "linux/arm/v7"}, true, true, nil, "", nil)
	require.NoError(t, err)

	require.Equal(t, 2, len(ng.Nodes))

	// update
	err = ng.Update("foo", "foo2", []string{"linux/amd64", "linux/arm"}, true, false, nil, "", nil)
	require.NoError(t, err)

	require.Equal(t, 2, len(ng.Nodes))
	require.Equal(t, []string{"linux/amd64", "linux/arm/v7"}, platformutil.Format(ng.Nodes[0].Platforms))
	require.Equal(t, []string{"linux/arm64"}, platformutil.Format(ng.Nodes[1].Platforms))

	require.Equal(t, "foo2", ng.Nodes[0].Endpoint)
	require.Equal(t, []string{"--debug"}, ng.Nodes[0].BuildkitdFlags)
	require.Equal(t, []string(nil), ng.Nodes[1].BuildkitdFlags)

	// duplicate endpoint
	err = ng.Update("foo1", "foo2", nil, true, false, nil, "", nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "duplicate endpoint")

	err = ng.Leave("foo")
	require.NoError(t, err)

	require.Equal(t, 1, len(ng.Nodes))
	require.Equal(t, []string{"linux/arm64"}, platformutil.Format(ng.Nodes[0].Platforms))
}
