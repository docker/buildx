package imagetools

import (
	"context"
	"encoding/base64"
	"fmt"
	"testing"

	"github.com/opencontainers/go-digest"

	"github.com/stretchr/testify/assert"
)

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
			r.refs["sha256:linux/amd64"] = []digest.Digest{
				"sha256:linux/amd64-attestation",
			}
			a := asset{}
			loader.scanSBOM(ctx, fetcher, r, r.refs["sha256:linux/amd64"], &a)
			r.assets["linux/amd64"] = a
			actual, err := r.SBOM()

			assert.NoError(t, err)
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

			r.refs["sha256:linux/amd64"] = []digest.Digest{
				"sha256:linux/amd64-attestation",
			}

			a := asset{}
			loader.scanProvenance(ctx, fetcher, r, r.refs["sha256:linux/amd64"], &a)
			r.assets["linux/amd64"] = a
			actual, err := r.Provenance()

			assert.NoError(t, err)
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
			assert.Equal(t, isInTotoDSSE(test.mime), test.expected)
		})
	}
}

func Test_decodeDSSE(t *testing.T) {
	// Returns input when mime isn't a DSSE type
	actual, err := decodeDSSE([]byte("foobar"), "application/vnd.in-toto+json")
	assert.NoError(t, err)
	assert.Equal(t, []byte("foobar"), actual)

	// Returns the base64 decoded payload if is a DSSE
	payload := base64.StdEncoding.EncodeToString([]byte("hello world"))
	envelope := fmt.Sprintf("{\"payload\":\"%s\"}", payload)
	actual, err = decodeDSSE([]byte(envelope), "application/vnd.in-toto.spdx+dsse")
	assert.NoError(t, err)
	assert.Equal(t, "hello world", string(actual))

	_, err = decodeDSSE([]byte("not a json"), "application/vnd.in-toto.spdx+dsse")
	assert.Error(t, err)

	_, err = decodeDSSE([]byte("{\"payload\": \"not base64\"}"), "application/vnd.in-toto.spdx+dsse")
	assert.Error(t, err)
}
