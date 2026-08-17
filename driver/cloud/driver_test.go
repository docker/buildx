package cloud

import (
	"fmt"
	"testing"

	"github.com/moby/moby/api/types/system"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type registryTokenSupport struct {
	os       string
	version  string
	driver   string
	expected bool
}

func TestRegistryTokenSupport(t *testing.T) {
	t.Parallel()

	table := []registryTokenSupport{
		{
			os:       "Docker Desktop",
			version:  "master",
			driver:   "io.containerd.snapshotter.v1",
			expected: true,
		},
		{
			os:       "Linux Mint 21.1",
			version:  "25.0.0",
			driver:   "io.containerd.snapshotter.v1",
			expected: true,
		},
		{
			os:       "Linux Mint 21.1",
			version:  "28.0.0-rc.1",
			driver:   "io.containerd.snapshotter.v1",
			expected: true,
		},
		{
			os:       "Linux Mint 21.1",
			version:  "25.0.0-rc.1",
			driver:   "io.containerd.snapshotter.v1",
			expected: false,
		},
		{
			os:       "Linux Mint 21.1",
			version:  "24.0.6",
			driver:   "io.containerd.snapshotter.v1",
			expected: false,
		},
		{
			os:       "Linux Mint 21.1",
			version:  "master",
			driver:   "io.containerd.snapshotter.v1",
			expected: true,
		},
		{
			os:       "Linux Mint 21.1",
			version:  "24.0.6",
			driver:   "docker",
			expected: true,
		},
	}

	for i, c := range table {
		t.Run(fmt.Sprintf("case%d", i), func(t *testing.T) {
			t.Parallel()

			actual, err := daemonInfoSupportsRegistryTokenPull(system.Info{
				ServerVersion:   c.version,
				OperatingSystem: c.os,
				DriverStatus: [][2]string{
					{"driver-type", c.driver},
				},
			})
			require.NoError(t, err)
			assert.Equal(t, c.expected, actual)
		})
	}
}
