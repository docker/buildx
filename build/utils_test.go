package build

import (
	"context"
	"strings"
	"testing"

	dockeropts "github.com/docker/cli/opts"
	"github.com/stretchr/testify/require"
)

func TestToBuildkitExtraHosts(t *testing.T) {
	tests := []struct {
		doc         string
		input       []string
		expectedOut string // Expect output==input if not set.
		expectedErr string // Expect success if not set.
	}{
		{
			doc:         "IPv4, colon sep",
			input:       []string{`myhost:192.168.0.1`},
			expectedOut: `myhost=192.168.0.1`,
		},
		{
			doc:   "IPv4, eq sep",
			input: []string{`myhost=192.168.0.1`},
		},
		{
			doc:         "Weird but permitted, IPv4 with brackets",
			input:       []string{`myhost=[192.168.0.1]`},
			expectedOut: `myhost=192.168.0.1`,
		},
		{
			doc:         "Host and domain",
			input:       []string{`host.and.domain.invalid:10.0.2.1`},
			expectedOut: `host.and.domain.invalid=10.0.2.1`,
		},
		{
			doc:         "IPv6, colon sep",
			input:       []string{`anipv6host:2003:ab34:e::1`},
			expectedOut: `anipv6host=2003:ab34:e::1`,
		},
		{
			doc:         "IPv6, colon sep, brackets",
			input:       []string{`anipv6host:[2003:ab34:e::1]`},
			expectedOut: `anipv6host=2003:ab34:e::1`,
		},
		{
			doc:         "IPv6, eq sep, brackets",
			input:       []string{`anipv6host=[2003:ab34:e::1]`},
			expectedOut: `anipv6host=2003:ab34:e::1`,
		},
		{
			doc:         "IPv6 localhost, colon sep",
			input:       []string{`ipv6local:::1`},
			expectedOut: `ipv6local=::1`,
		},
		{
			doc:   "IPv6 localhost, eq sep",
			input: []string{`ipv6local=::1`},
		},
		{
			doc:         "IPv6 localhost, eq sep, brackets",
			input:       []string{`ipv6local=[::1]`},
			expectedOut: `ipv6local=::1`,
		},
		{
			doc:         "IPv6 localhost, non-canonical, colon sep",
			input:       []string{`ipv6local:0:0:0:0:0:0:0:1`},
			expectedOut: `ipv6local=0:0:0:0:0:0:0:1`,
		},
		{
			doc:   "IPv6 localhost, non-canonical, eq sep",
			input: []string{`ipv6local=0:0:0:0:0:0:0:1`},
		},
		{
			doc:         "Multi IPs",
			input:       []string{`myhost=162.242.195.82,162.242.195.83`},
			expectedOut: `myhost=162.242.195.82,myhost=162.242.195.83`,
		},
		{
			doc:         "IPv6 localhost, non-canonical, eq sep, brackets",
			input:       []string{`ipv6local=[0:0:0:0:0:0:0:1]`},
			expectedOut: `ipv6local=0:0:0:0:0:0:0:1`,
		},
		{
			doc:         "Bad address, colon sep",
			input:       []string{`myhost:192.notanipaddress.1`},
			expectedErr: `invalid IP address in add-host: "192.notanipaddress.1"`,
		},
		{
			doc:         "Bad address, eq sep",
			input:       []string{`myhost=192.notanipaddress.1`},
			expectedErr: `invalid IP address in add-host: "192.notanipaddress.1"`,
		},
		{
			doc:         "No sep",
			input:       []string{`thathost-nosemicolon10.0.0.1`},
			expectedErr: `bad format for add-host: "thathost-nosemicolon10.0.0.1"`,
		},
		{
			doc:         "Bad IPv6",
			input:       []string{`anipv6host:::::1`},
			expectedErr: `invalid IP address in add-host: "::::1"`,
		},
		{
			doc:         "Bad IPv6, trailing colons",
			input:       []string{`ipv6local:::0::`},
			expectedErr: `invalid IP address in add-host: "::0::"`,
		},
		{
			doc:         "Bad IPv6, missing close bracket",
			input:       []string{`ipv6addr=[::1`},
			expectedErr: `invalid IP address in add-host: "[::1"`,
		},
		{
			doc:         "Bad IPv6, missing open bracket",
			input:       []string{`ipv6addr=::1]`},
			expectedErr: `invalid IP address in add-host: "::1]"`,
		},
		{
			doc:         "Missing address, colon sep",
			input:       []string{`myhost.invalid:`},
			expectedErr: `invalid IP address in add-host: ""`,
		},
		{
			doc:         "Missing address, eq sep",
			input:       []string{`myhost.invalid=`},
			expectedErr: `invalid IP address in add-host: ""`,
		},
		{
			doc:         "No input",
			input:       []string{``},
			expectedErr: `bad format for add-host: ""`,
		},
	}

	for _, tc := range tests {
		if tc.expectedOut == "" {
			tc.expectedOut = strings.Join(tc.input, ",")
		}
		t.Run(tc.doc, func(t *testing.T) {
			actualOut, actualErr := toBuildkitExtraHosts(context.TODO(), tc.input, nil)
			if tc.expectedErr == "" {
				require.Equal(t, tc.expectedOut, actualOut)
				require.NoError(t, actualErr)
			} else {
				require.Zero(t, actualOut)
				require.Error(t, actualErr, tc.expectedErr)
			}
		})
	}
}

func TestAddResourceLimits(t *testing.T) {
	mustMemSwap := func(v string) dockeropts.MemSwapBytes {
		var m dockeropts.MemSwapBytes
		require.NoError(t, m.Set(v))
		return m
	}

	tests := []struct {
		name     string
		limits   ResourceLimits
		expected map[string]string
	}{
		{
			name:     "empty",
			limits:   ResourceLimits{},
			expected: map[string]string{},
		},
		{
			name: "all",
			limits: ResourceLimits{
				Memory:     dockeropts.MemBytes(2 * 1024 * 1024 * 1024),
				MemorySwap: mustMemSwap("4g"),
				CPUShares:  1024,
				CPUPeriod:  100000,
				CPUQuota:   50000,
				CPUSetCPUs: "0-3",
				CPUSetMems: "0,1",
			},
			expected: map[string]string{
				"memory":     "2147483648",
				"memswap":    "4294967296",
				"cpushares":  "1024",
				"cpuperiod":  "100000",
				"cpuquota":   "50000",
				"cpusetcpus": "0-3",
				"cpusetmems": "0,1",
			},
		},
		{
			name:   "unlimited swap",
			limits: ResourceLimits{MemorySwap: mustMemSwap("-1")},
			expected: map[string]string{
				"memswap": "-1",
			},
		},
		{
			name:   "partial",
			limits: ResourceLimits{Memory: dockeropts.MemBytes(512 * 1024 * 1024)},
			expected: map[string]string{
				"memory": "536870912",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			attrs := map[string]string{}
			addResourceLimits(tc.limits, attrs)
			require.Equal(t, tc.expected, attrs)
		})
	}
}

func TestParseResourceLimits(t *testing.T) {
	t.Run("all", func(t *testing.T) {
		rl, err := ParseResourceLimits([]string{
			"memory=2g",
			"memory-swap=4g",
			"cpu-shares=1024",
			"cpu-period=100000",
			"cpu-quota=50000",
			"cpuset-cpus=0-3",
			"cpuset-mems=0,1",
		})
		require.NoError(t, err)
		require.Equal(t, int64(2*1024*1024*1024), rl.Memory.Value())
		require.Equal(t, int64(4*1024*1024*1024), rl.MemorySwap.Value())
		require.Equal(t, int64(1024), rl.CPUShares)
		require.Equal(t, int64(100000), rl.CPUPeriod)
		require.Equal(t, int64(50000), rl.CPUQuota)
		require.Equal(t, "0-3", rl.CPUSetCPUs)
		require.Equal(t, "0,1", rl.CPUSetMems)
	})

	t.Run("unlimited swap", func(t *testing.T) {
		rl, err := ParseResourceLimits([]string{"memory-swap=-1"})
		require.NoError(t, err)
		require.Equal(t, int64(-1), rl.MemorySwap.Value())
	})

	t.Run("missing value", func(t *testing.T) {
		_, err := ParseResourceLimits([]string{"memory"})
		require.Error(t, err)
	})

	t.Run("unknown key", func(t *testing.T) {
		_, err := ParseResourceLimits([]string{"bogus=1"})
		require.Error(t, err)
	})

	t.Run("invalid int", func(t *testing.T) {
		_, err := ParseResourceLimits([]string{"cpu-shares=notanumber"})
		require.Error(t, err)
	})
}
