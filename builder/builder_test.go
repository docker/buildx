package builder

import (
	"os"
	"path"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCsvToMap(t *testing.T) {
	d := []string{
		"\"tolerations=key=foo,value=bar;key=foo2,value=bar2\",replicas=1",
		"namespace=default",
	}
	r, err := csvToMap(d)

	require.NoError(t, err)

	require.Contains(t, r, "tolerations")
	require.Equal(t, "key=foo,value=bar;key=foo2,value=bar2", r["tolerations"])

	require.Contains(t, r, "replicas")
	require.Equal(t, "1", r["replicas"])

	require.Contains(t, r, "namespace")
	require.Equal(t, "default", r["namespace"])
}

func TestParseBuildkitdFlags(t *testing.T) {
	dirConf := t.TempDir()

	buildkitdConfPath := path.Join(dirConf, "buildkitd-conf.toml")
	require.NoError(t, os.WriteFile(buildkitdConfPath, []byte(`
# debug enables additional debug logging
debug = true
# insecure-entitlements allows insecure entitlements, disabled by default.
insecure-entitlements = [ "network.host", "security.insecure" ]
[log]
  # log formatter: json or text
  format = "text"
`), 0644))

	buildkitdConfBrokenPath := path.Join(dirConf, "buildkitd-conf-broken.toml")
	require.NoError(t, os.WriteFile(buildkitdConfBrokenPath, []byte(`
[worker.oci]
  gc = "maybe"
`), 0644))

	buildkitdConfUnknownFieldPath := path.Join(dirConf, "buildkitd-unknown-field.toml")
	require.NoError(t, os.WriteFile(buildkitdConfUnknownFieldPath, []byte(`
foo = "bar"
`), 0644))

	testCases := []struct {
		name                string
		flags               string
		driver              string
		driverOpts          map[string]string
		buildkitdConfigFile string
		expected            []string
		wantErr             bool
	}{
		{
			"docker-container no flags",
			"",
			"docker-container",
			nil,
			"",
			[]string{
				"--allow-insecure-entitlement=network.host",
			},
			false,
		},
		{
			"kubernetes no flags",
			"",
			"kubernetes",
			nil,
			"",
			[]string{
				"--allow-insecure-entitlement=network.host",
			},
			false,
		},
		{
			"remote no flags",
			"",
			"remote",
			nil,
			"",
			nil,
			false,
		},
		{
			"docker-container with insecure flag",
			"--allow-insecure-entitlement=security.insecure",
			"docker-container",
			nil,
			"",
			[]string{
				"--allow-insecure-entitlement=security.insecure",
			},
			false,
		},
		{
			"docker-container with insecure and host flag",
			"--allow-insecure-entitlement=network.host --allow-insecure-entitlement=security.insecure",
			"docker-container",
			nil,
			"",
			[]string{
				"--allow-insecure-entitlement=network.host",
				"--allow-insecure-entitlement=security.insecure",
			},
			false,
		},
		{
			"docker-container with network host opt",
			"",
			"docker-container",
			map[string]string{"network": "host"},
			"",
			[]string{
				"--allow-insecure-entitlement=network.host",
			},
			false,
		},
		{
			"docker-container with host flag and network host opt",
			"--allow-insecure-entitlement=network.host",
			"docker-container",
			map[string]string{"network": "host"},
			"",
			[]string{
				"--allow-insecure-entitlement=network.host",
			},
			false,
		},
		{
			"docker-container with insecure, host flag and network host opt",
			"--allow-insecure-entitlement=network.host --allow-insecure-entitlement=security.insecure",
			"docker-container",
			map[string]string{"network": "host"},
			"",
			[]string{
				"--allow-insecure-entitlement=network.host",
				"--allow-insecure-entitlement=security.insecure",
			},
			false,
		},
		{
			"docker-container with buildkitd conf setting network.host entitlement",
			"",
			"docker-container",
			nil,
			buildkitdConfPath,
			nil,
			false,
		},
		{
			"error parsing flags",
			"foo'",
			"docker-container",
			nil,
			"",
			nil,
			true,
		},
		{
			"error parsing buildkit config",
			"",
			"docker-container",
			nil,
			buildkitdConfBrokenPath,
			nil,
			true,
		},
		{
			"unknown field in buildkit config",
			"",
			"docker-container",
			nil,
			buildkitdConfUnknownFieldPath,
			[]string{
				"--allow-insecure-entitlement=network.host",
			},
			false,
		},
	}
	for _, tt := range testCases {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			flags, err := parseBuildkitdFlags(tt.flags, tt.driver, tt.driverOpts, tt.buildkitdConfigFile)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.expected, flags)
		})
	}
}
