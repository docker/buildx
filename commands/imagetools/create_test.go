package commands

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseSourceDescriptorValidation(t *testing.T) {
	t.Parallel()

	_, err := parseSource(`{"schemaVersion":2,"mediaType":"application/vnd.docker.distribution.manifest.list.v2+json","manifests":[]}`)
	require.Error(t, err)
	require.Contains(t, err.Error(), "expected an OCI content descriptor")

	_, err = parseSource(`{"mediaType":"application/vnd.oci.image.manifest.v1+json"}`)
	require.Error(t, err)
	require.Contains(t, err.Error(), "digest is required")

	_, err = parseSource(`{"digest":"not-a-digest","mediaType":"application/vnd.oci.image.manifest.v1+json","size":123}`)
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid descriptor digest")

	src, err := parseSource(`{"digest":"sha256:0000000000000000000000000000000000000000000000000000000000000000","mediaType":"application/vnd.oci.image.manifest.v1+json","size":123}`)
	require.NoError(t, err)
	require.Equal(t, "sha256:0000000000000000000000000000000000000000000000000000000000000000", src.Desc.Digest.String())
}
