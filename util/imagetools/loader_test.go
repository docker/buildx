package imagetools

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

	"github.com/containerd/containerd/v2/core/content"
	"github.com/containerd/errdefs"
	"github.com/opencontainers/go-digest"
	ocispecs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/pkg/errors"

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

// blobMap is a content.Provider that serves whatever bytes are stored for a
// digest, like a registry fetcher or an OCI layout store would.
type blobMap map[digest.Digest][]byte

func (m blobMap) ReaderAt(_ context.Context, desc ocispecs.Descriptor) (content.ReaderAt, error) {
	dt, ok := m[desc.Digest]
	if !ok {
		return nil, errors.WithStack(errdefs.ErrNotFound)
	}
	return &bytesReaderAt{Reader: bytes.NewReader(dt)}, nil
}

type bytesReaderAt struct {
	*bytes.Reader
}

func (r *bytesReaderAt) Close() error { return nil }

func TestReadProvenancePredicateVerifiesDigests(t *testing.T) {
	stmt := []byte(`{"_type":"https://in-toto.io/Statement/v1","predicateType":"https://slsa.dev/provenance/v1","predicate":{"buildDefinition":{"buildType":"recorded"}}}`)
	layer := ocispecs.Descriptor{
		MediaType:   inTotoGenericMime,
		Digest:      digest.FromBytes(stmt),
		Size:        int64(len(stmt)),
		Annotations: map[string]string{"in-toto.io/predicate-type": "https://slsa.dev/provenance/v1"},
	}
	mfstDt, err := json.Marshal(ocispecs.Manifest{MediaType: ocispecs.MediaTypeImageManifest, Layers: []ocispecs.Descriptor{layer}})
	require.NoError(t, err)
	mfst := ocispecs.Descriptor{
		MediaType: ocispecs.MediaTypeImageManifest,
		Digest:    digest.FromBytes(mfstDt),
		Size:      int64(len(mfstDt)),
	}

	provider := blobMap{mfst.Digest: mfstDt, layer.Digest: stmt}
	pred, predType, err := ReadProvenancePredicate(context.Background(), provider, mfst)
	require.NoError(t, err)
	require.Equal(t, "https://slsa.dev/provenance/v1", predType)
	require.JSONEq(t, `{"buildDefinition":{"buildType":"recorded"}}`, string(pred))

	// A provider serving different content of the same size for the recorded
	// digest must not be trusted.
	tampered := bytes.Replace(stmt, []byte("recorded"), []byte("replaced"), 1)
	require.Len(t, tampered, len(stmt))
	_, _, err = ReadProvenancePredicate(context.Background(), blobMap{mfst.Digest: mfstDt, layer.Digest: tampered}, mfst)
	require.ErrorContains(t, err, "digest mismatch")

	tamperedMfst := bytes.Replace(mfstDt, []byte(`"layers"`), []byte(`"Layers"`), 1)
	_, _, err = ReadProvenancePredicate(context.Background(), blobMap{mfst.Digest: tamperedMfst, layer.Digest: stmt}, mfst)
	require.ErrorContains(t, err, "digest mismatch")

	_, err = ReadBlobVerified(context.Background(), blobMap{layer.Digest: stmt[:len(stmt)-1]}, layer)
	require.ErrorContains(t, err, "size mismatch")
}
