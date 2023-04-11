package docker

import (
	"testing"

	"github.com/Masterminds/semver/v3"
	"github.com/stretchr/testify/require"
)

func TestConstraint(t *testing.T) {
	for _, tt := range mobyBuildkitVersions {
		tt := tt
		t.Run(tt.MobyVersionConstraint, func(t *testing.T) {
			_, err := semver.NewConstraint(tt.MobyVersionConstraint)
			require.NoError(t, err)
		})
	}
}

func TestResolveBuildKitVersion(t *testing.T) {
	cases := []struct {
		mobyVersion string
		expected    string
		err         bool
	}{
		{
			mobyVersion: "18.06.1-ce",
			expected:    "v0.0.0+98f1604",
		},
		{
			mobyVersion: "18.09.1-beta1",
			expected:    "v0.3.3",
		},
		{
			mobyVersion: "19.03.0-beta1",
			expected:    "v0.4.0+b302896",
		},
		{
			mobyVersion: "19.03.5-beta2",
			expected:    "v0.6.2+ff93519",
		},
		{
			mobyVersion: "19.03.13-beta1",
			expected:    "v0.6.4+da1f4bf",
		},
		{
			mobyVersion: "19.03.13-beta2",
			expected:    "v0.6.4+da1f4bf",
		},
		{
			mobyVersion: "19.03.13",
			expected:    "v0.6.4+df89d4d",
		},
		{
			mobyVersion: "20.10.3-rc.1",
			expected:    "v0.8.1+68bb095",
		},
		{
			mobyVersion: "20.10.3",
			expected:    "v0.8.1+68bb095",
		},
		{
			mobyVersion: "20.10.4",
			expected:    "v0.8.2",
		},
		{
			mobyVersion: "20.10.16",
			expected:    "v0.8.2+bc07b2b8",
		},
		{
			mobyVersion: "20.10.19",
			expected:    "v0.8.2+3a1eeca5",
		},
		{
			mobyVersion: "20.10.23",
			expected:    "v0.8.2+eeb7b65",
		},
		{
			mobyVersion: "20.10.24",
			expected:    "v0.8+unknown",
		},
		{
			mobyVersion: "20.10.50",
			expected:    "v0.8+unknown",
		},
		{
			mobyVersion: "22.06.0-beta.0",
			expected:    "v0.10.3",
		},
		{
			mobyVersion: "22.06.0",
			expected:    "v0.10.3",
		},
		{
			mobyVersion: "23.0.0-rc.4",
			expected:    "v0.10.6",
		},
		{
			mobyVersion: "23.0.0",
			expected:    "v0.10.6",
		},
		{
			mobyVersion: "23.0.1",
			expected:    "v0.10.6+4f0ee09",
		},
		{
			mobyVersion: "23.0.2-rc.1",
			expected:    "v0.10.6+70f2ad5",
		},
		{
			mobyVersion: "23.0.3",
			expected:    "v0.10.6+70f2ad5",
		},
		{
			mobyVersion: "23.0.4",
			expected:    "v0.10+unknown",
		},
	}
	for _, tt := range cases {
		tt := tt
		t.Run(tt.mobyVersion, func(t *testing.T) {
			bkVersion, err := resolveBuildKitVersion(tt.mobyVersion)
			if tt.err {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.expected, bkVersion)
		})
	}
}
