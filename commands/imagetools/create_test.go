package commands

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestParseSource_manifestRejected is a regression test for
// https://github.com/docker/buildx/issues/2091: piping `imagetools inspect
// --raw` (a manifest/manifest-list JSON) into `imagetools create -f` caused a
// nil-pointer panic because the JSON was accepted as a descriptor but had no
// 'digest' field, producing a zero-value descriptor that drove the pusher to
// write 0 bytes and call Commit() on an uninitialised pipe.
func TestParseSource_manifestRejected(t *testing.T) {
	t.Parallel()

	cases := []struct{ name, in string }{
		{
			"docker manifest list",
			`{"schemaVersion":2,"mediaType":"application/vnd.docker.distribution.manifest.list.v2+json","manifests":[]}`,
		},
		{
			"oci image index",
			`{"schemaVersion":2,"mediaType":"application/vnd.oci.image.index.v1+json","manifests":[]}`,
		},
		{
			"descriptor missing digest",
			`{"mediaType":"application/vnd.oci.image.index.v1+json","size":256}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := parseSource(tc.in)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "descriptor is missing required 'digest' field")
		})
	}
}

func TestParseSource_validInputs(t *testing.T) {
	t.Parallel()

	// plain digest
	src, err := parseSource("sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855")
	require.NoError(t, err)
	assert.Equal(t, "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855", src.Desc.Digest.String())

	// registry reference
	src, err = parseSource("registry.example.com/myimage:latest")
	require.NoError(t, err)
	require.NotNil(t, src.Ref)
	assert.Equal(t, "registry.example.com/myimage:latest", src.Ref.String())

	// descriptor JSON
	src, err = parseSource(`{"mediaType":"application/vnd.oci.image.index.v1+json","digest":"sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855","size":256}`)
	require.NoError(t, err)
	assert.Equal(t, "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855", src.Desc.Digest.String())
}
