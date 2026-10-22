package ocilayout

import (
	"testing"

	digest "github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParse(t *testing.T) {
	for _, tt := range []struct {
		s    string
		path string
		dgst string
		tag  string
	}{
		{
			s:    "oci-layout:///tmp/cache@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/layout",
			path: "/tmp/cache@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/layout",
			tag:  "latest",
		},
		{
			s:    "oci-layout:///tmp/build:cache/layout",
			path: "/tmp/build:cache/layout",
			tag:  "latest",
		},
		{
			s:    "oci-layout:///tmp/build:cache/layout:1.3",
			path: "/tmp/build:cache/layout",
			tag:  "1.3",
		},
		{
			s:    "oci-layout:///path/to/oci/layout",
			path: "/path/to/oci/layout",
			tag:  "latest",
		},
		{
			s:    "oci-layout:///path/to/oci/layout:1.3",
			path: "/path/to/oci/layout",
			tag:  "1.3",
		},
		{
			s:    "oci-layout:///path/to/oci/layout@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			path: "/path/to/oci/layout",
			dgst: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		},
		{
			s:    "oci-layout:///path/to/oci/@/layout@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			path: "/path/to/oci/@/layout",
			dgst: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		},
		{
			s:    "oci-layout:///path/to/oci/@/layout",
			path: "/path/to/oci/@/layout",
			tag:  "latest",
		},
		{
			s:    "oci-layout://a:1.3",
			path: "a",
			tag:  "1.3",
		},
		{
			s:    `oci-layout://C:\path\to\oci\layout`,
			path: `C:\path\to\oci\layout`,
			tag:  "latest",
		},
		{
			s:    `oci-layout://C:/path/to/oci/layout`,
			path: `C:/path/to/oci/layout`,
			tag:  "latest",
		},
		{
			s:    `oci-layout://C:\path\to\oci\layout:1.3`,
			path: `C:\path\to\oci\layout`,
			tag:  "1.3",
		},
		{
			s:    `oci-layout://C:\path\to\oci\layout@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa`,
			path: `C:\path\to\oci\layout`,
			dgst: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		},
		{
			s:    `oci-layout://C:\path\to\oci\layout:1.3@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa`,
			path: `C:\path\to\oci\layout`,
			tag:  "1.3",
			dgst: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		},
	} {
		t.Run(tt.s, func(t *testing.T) {
			ref, ok, err := Parse(tt.s)
			require.True(t, ok)
			require.NoError(t, err)
			assert.Equal(t, tt.path, ref.Path, "comparing path: %s", tt.s)
			assert.Equal(t, tt.dgst, ref.Digest.String(), "comparing digest: %s", tt.s)
			assert.Equal(t, tt.tag, ref.Tag, "comparing tag: %s", tt.s)
		})
	}
}

func TestRefString(t *testing.T) {
	ref := Ref{
		Path:   "/path/to/oci/layout",
		Tag:    "1.3",
		Digest: digest.Digest("sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
	}

	assert.Equal(t, "oci-layout:///path/to/oci/layout:1.3@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", ref.String())
}
