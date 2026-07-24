package cloud

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetBuilderInstanceDriverOpts(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name            string
		driverOpts      map[string]string
		builderInstance CloudBuilder
		expected        map[string]string
	}{
		{
			name:       "NoOptsOrDataplane",
			driverOpts: map[string]string{},
			expected:   map[string]string{},
		},
		{
			name: "DataPlaneOnly",
			builderInstance: CloudBuilder{
				DataPlane: CloudBuilderDataPlane{
					ProxyEndpoint:    "tcp://endpoint:443",
					RegistryEndpoint: "https://endpoint:443",
					HealthEndpoint:   "https://endpoint:443/v2/",
				},
			},
			driverOpts: map[string]string{
				"a": "b",
			},
			expected: map[string]string{
				"a":                               "b",
				"internal.cloud.proxy.address":    "tcp://endpoint:443",
				"internal.cloud.registry.address": "https://endpoint:443",
				"internal.cloud.health.address":   "https://endpoint:443/v2/",
			},
		},
		{
			name: "DriverOptsOnly",
			driverOpts: map[string]string{
				"a":                "b",
				"internal.address": "c",
			},
			expected: map[string]string{
				"a":                "b",
				"internal.address": "c",
			},
		},
		{
			name: "InternalAddressOverridesDataPlane",
			builderInstance: CloudBuilder{
				DataPlane: CloudBuilderDataPlane{
					ProxyEndpoint:    "tcp://endpoint:443",
					RegistryEndpoint: "https://endpoint:443",
					HealthEndpoint:   "https://endpoint:443/v2/",
				},
			},
			driverOpts: map[string]string{
				"a":                "b",
				"internal.address": "c",
			},
			expected: map[string]string{
				"a":                "b",
				"internal.address": "c",
			},
		},
		{
			name: "PartialDataPlaneOverride",
			builderInstance: CloudBuilder{
				DataPlane: CloudBuilderDataPlane{
					ProxyEndpoint:    "tcp://endpoint:443",
					RegistryEndpoint: "https://endpoint:443",
					HealthEndpoint:   "https://endpoint:443/v2/",
				},
			},
			driverOpts: map[string]string{
				"a":                            "b",
				"internal.cloud.proxy.address": "tcp://endpoint2:443",
			},
			expected: map[string]string{
				"a":                               "b",
				"internal.cloud.proxy.address":    "tcp://endpoint2:443",
				"internal.cloud.registry.address": "https://endpoint:443",
				"internal.cloud.health.address":   "https://endpoint:443/v2/",
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result := GetBuilderInstanceDriverOpts(tc.builderInstance, tc.driverOpts)

			assert.Equal(t, tc.expected, result)
		})
	}
}
