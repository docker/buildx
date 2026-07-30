package cloud

import (
	"testing"

	"github.com/docker/buildx/driver"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseBuilderName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		builder     string
		wantAccount string
		wantName    string
		wantErr     bool
	}{
		{
			name:        "Group",
			builder:     "org/builder",
			wantAccount: "org",
			wantName:    "builder",
		},
		{
			name:        "Endpoint",
			builder:     "cloud://org/builder",
			wantAccount: "org",
			wantName:    "builder",
		},
		{
			name:    "MissingName",
			builder: "org",
			wantErr: true,
		},
		{
			name:    "EmptyAccount",
			builder: "/builder",
			wantErr: true,
		},
		{
			name:    "EmptyName",
			builder: "org/",
			wantErr: true,
		},
		{
			name:    "TooManyParts",
			builder: "org/builder/extra",
			wantErr: true,
		},
		{
			name:    "DotAccount",
			builder: "./builder",
			wantErr: true,
		},
		{
			name:    "DotDotAccount",
			builder: "../builder",
			wantErr: true,
		},
		{
			name:    "DotName",
			builder: "org/.",
			wantErr: true,
		},
		{
			name:    "DotDotName",
			builder: "org/..",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			account, name, err := parseBuilderName(tt.builder)
			if tt.wantErr {
				require.EqualError(t, err, "builder should be in the format: <account>/<builder>")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantAccount, account)
			assert.Equal(t, tt.wantName, name)
		})
	}
}

func TestResolveNodesRequiresEndpoint(t *testing.T) {
	t.Parallel()

	_, err := (&factory{}).ResolveNodes(t.Context(), driver.Node{})
	require.EqualError(t, err, "no endpoint (builder) provided")
}

func TestResolveNodesRejectsMalformedEndpoint(t *testing.T) {
	t.Parallel()

	_, err := (&factory{}).ResolveNodes(t.Context(), driver.Node{Endpoint: "org"})
	require.EqualError(t, err, "builder should be in the format: <account>/<builder>")
}

func TestGetBuilderInstancesRejectsMalformedBuilder(t *testing.T) {
	t.Parallel()

	_, err := getBuilderInstances(t.Context(), "org", "http://example.com", "token")
	require.EqualError(t, err, "builder should be in the format: <account>/<builder>")
}

func TestGetBuilderInstanceDriverOpts(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name            string
		driverOpts      map[string]string
		builderInstance cloudBuilder
		expected        map[string]string
	}{
		{
			name:       "NoOptsOrDataplane",
			driverOpts: map[string]string{},
			expected:   map[string]string{},
		},
		{
			name: "DataPlaneOnly",
			builderInstance: cloudBuilder{
				DataPlane: cloudBuilderDataPlane{
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
			builderInstance: cloudBuilder{
				DataPlane: cloudBuilderDataPlane{
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
			builderInstance: cloudBuilder{
				DataPlane: cloudBuilderDataPlane{
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

			result := getBuilderInstanceDriverOpts(tc.builderInstance, tc.driverOpts)

			assert.Equal(t, tc.expected, result)
		})
	}
}
