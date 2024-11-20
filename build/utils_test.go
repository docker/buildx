package build

import (
	"context"
	"strings"
	"testing"

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
		tc := tc
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
