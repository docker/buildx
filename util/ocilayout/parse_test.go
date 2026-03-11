package ocilayout

import (
	"testing"

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
	} {
		ref, ok, err := Parse(tt.s)
		require.True(t, ok)
		require.NoError(t, err)
		assert.Equal(t, tt.path, ref.Path, "comparing path: %s", tt.s)
		assert.Equal(t, tt.dgst, ref.Digest.String(), "comparing digest: %s", tt.s)
		assert.Equal(t, tt.tag, ref.Tag, "comparing tag: %s", tt.s)
	}
}
