package imagetools

import (
	"context"
	"encoding/base64"
	"fmt"
	"reflect"
	"testing"

	"github.com/opencontainers/go-digest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoad(t *testing.T) {
	loader := newLoader(getMockResolver())
	ctx := context.Background()

	r := getImageNoAttestation()
	indexDigest := reflect.ValueOf(r.indexes).MapKeys()[0].String()
	result, err := loader.Load(ctx, fmt.Sprintf("test@%s", indexDigest))
	require.NoError(t, err)
	if err == nil {
		assert.Equal(t, 1, len(result.indexes))
		assert.Equal(t, 2, len(result.images))
		assert.Equal(t, 2, len(result.platforms))
		assert.Equal(t, 2, len(result.manifests))
		assert.Equal(t, 2, len(result.assets))
		assert.Equal(t, 0, len(result.refs))
	}

	r = getImageWithAttestation(plainSpdx)
	indexDigest = reflect.ValueOf(r.indexes).MapKeys()[0].String()
	result, err = loader.Load(ctx, fmt.Sprintf("test@%s", indexDigest))
	require.NoError(t, err)
	if err == nil {
		assert.Equal(t, 1, len(result.indexes))
		assert.Equal(t, 2, len(result.images))
		assert.Equal(t, 2, len(result.platforms))
		assert.Equal(t, 4, len(result.manifests))
		assert.Equal(t, 2, len(result.assets))
		assert.Equal(t, 2, len(result.refs))

		for d1, m := range r.manifests {
			if _, ok := m.desc.Annotations["vnd.docker.reference.digest"]; ok {
				d2 := digest.Digest(m.desc.Annotations["vnd.docker.reference.digest"])
				assert.Equal(t, d1, result.refs[d2][0])
			}
		}
	}
}

func TestSBOM(t *testing.T) {
	tests := []struct {
		name        string
		contentType attestationType
	}{
		{
			name:        "Plain SPDX",
			contentType: plainSpdx,
		},
		{
			name:        "SPDX in DSSE envelope",
			contentType: dsseEmbeded,
		},
		{
			name:        "Plain SPDX and SPDX in DSSE envelope",
			contentType: plainSpdxAndDSSEEmbed,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			loader := newLoader(getMockResolver())
			ctx := context.Background()
			fetcher, _ := loader.resolver.Fetcher(ctx, "")

			r := getImageWithAttestation(test.contentType)
			imageDigest := r.images["linux/amd64"]

			// Manual mapping
			for d, m := range r.manifests {
				if m.desc.Annotations["vnd.docker.reference.digest"] == string(imageDigest) {
					r.refs[imageDigest] = []digest.Digest{
						d,
					}
				}
			}

			a := asset{}
			loader.scanSBOM(ctx, fetcher, r, r.refs[imageDigest], &a)
			r.assets["linux/amd64"] = a
			actual, err := r.SBOM()

			require.NoError(t, err)
			assert.Equal(t, 1, len(actual))
		})
	}
}

func TestProvenance(t *testing.T) {
	tests := []struct {
		name        string
		contentType attestationType
	}{
		{
			name:        "Plain SPDX",
			contentType: plainSpdx,
		},
		{
			name:        "SPDX in DSSE envelope",
			contentType: dsseEmbeded,
		},
		{
			name:        "Plain SPDX and SPDX in DSSE envelope",
			contentType: plainSpdxAndDSSEEmbed,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			loader := newLoader(getMockResolver())
			ctx := context.Background()
			fetcher, _ := loader.resolver.Fetcher(ctx, "")

			r := getImageWithAttestation(test.contentType)
			imageDigest := r.images["linux/amd64"]

			// Manual mapping
			for d, m := range r.manifests {
				if m.desc.Annotations["vnd.docker.reference.digest"] == string(imageDigest) {
					r.refs[imageDigest] = []digest.Digest{
						d,
					}
				}
			}

			a := asset{}
			loader.scanProvenance(ctx, fetcher, r, r.refs[imageDigest], &a)
			r.assets["linux/amd64"] = a
			actual, err := r.Provenance()

			require.NoError(t, err)
			assert.Equal(t, 1, len(actual))
		})
	}
}

func Test_isInTotoDSSE(t *testing.T) {
	tests := []struct {
		mime     string
		expected bool
	}{
		{
			mime:     "application/vnd.in-toto.spdx+dsse",
			expected: true,
		},
		{
			mime:     "application/vnd.in-toto.provenance+dsse",
			expected: true,
		},
		{
			mime:     "application/vnd.in-toto+json",
			expected: false,
		},
	}

	for _, test := range tests {
		t.Run(test.mime, func(t *testing.T) {
			assert.Equal(t, test.expected, isInTotoDSSE(test.mime))
		})
	}
}

func Test_decodeDSSE(t *testing.T) {
	// Returns input when mime isn't a DSSE type
	actual, err := decodeDSSE([]byte("foobar"), "application/vnd.in-toto+json")
	require.NoError(t, err)
	assert.Equal(t, []byte("foobar"), actual)

	// Returns the base64 decoded payload if is a DSSE
	payload := base64.StdEncoding.EncodeToString([]byte("hello world"))
	envelope := fmt.Sprintf("{\"payload\":\"%s\"}", payload)
	actual, err = decodeDSSE([]byte(envelope), "application/vnd.in-toto.spdx+dsse")
	require.NoError(t, err)
	assert.Equal(t, "hello world", string(actual))

	_, err = decodeDSSE([]byte("not a json"), "application/vnd.in-toto.spdx+dsse")
	require.Error(t, err)

	_, err = decodeDSSE([]byte("{\"payload\": \"not base64\"}"), "application/vnd.in-toto.spdx+dsse")
	require.Error(t, err)
}
