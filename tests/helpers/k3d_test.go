package helpers

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestK3dRegistryConfigUsesHostPorts(t *testing.T) {
	cfg, cleanup := K3dRegistryConfig("172.19.0.1", []int{40001, 40002}).UpdateConfigFile("debug = true")
	if cleanup != nil {
		t.Cleanup(func() {
			require.NoError(t, cleanup())
		})
	}

	require.Contains(t, cfg, `debug = true`)
	require.Contains(t, cfg, `[registry."172.19.0.1:40001"]`)
	require.Contains(t, cfg, `[registry."172.19.0.1:40002"]`)
	require.NotContains(t, cfg, `[registry."172.19.0.1"]`)
}

func TestK3dRegistryConfigWithoutPortsIsNoop(t *testing.T) {
	cfg, cleanup := K3dRegistryConfig("172.19.0.1", nil).UpdateConfigFile("debug = true")
	require.Nil(t, cleanup)
	require.Equal(t, "debug = true", cfg)
}
