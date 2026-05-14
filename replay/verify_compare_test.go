package replay

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/containerd/containerd/v2/core/content"
	"github.com/moby/buildkit/util/contentutil"
	digest "github.com/opencontainers/go-digest"
	ocispecsgo "github.com/opencontainers/image-spec/specs-go"
	ocispecs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
)

func TestCompareDigestMatch(t *testing.T) {
	d := digest.FromBytes([]byte("hello"))
	subject := ocispecs.Descriptor{Digest: d, MediaType: ocispecs.MediaTypeImageManifest}
	replay := ocispecs.Descriptor{Digest: d, MediaType: ocispecs.MediaTypeImageManifest}
	require.True(t, CompareDigest(subject, replay))
}

func TestCompareDigestMismatch(t *testing.T) {
	subject := ocispecs.Descriptor{Digest: digest.FromBytes([]byte("a")), MediaType: ocispecs.MediaTypeImageManifest}
	replay := ocispecs.Descriptor{Digest: digest.FromBytes([]byte("b")), MediaType: ocispecs.MediaTypeImageManifest}
	require.False(t, CompareDigest(subject, replay))
	// Empty subject digest is never considered a match.
	require.False(t, CompareDigest(ocispecs.Descriptor{}, replay))
}

// writeManifestTree writes a minimal OCI manifest with the given config +
// layer blob contents into buf and returns the manifest descriptor.
func writeManifestTree(t *testing.T, buf contentutil.Buffer, configBytes []byte, layerBytes []byte) ocispecs.Descriptor {
	t.Helper()
	ctx := context.Background()

	configDesc := ocispecs.Descriptor{
		MediaType: ocispecs.MediaTypeImageConfig,
		Digest:    digest.FromBytes(configBytes),
		Size:      int64(len(configBytes)),
	}
	require.NoError(t, content.WriteBlob(ctx, buf, configDesc.Digest.String(), bytes.NewReader(configBytes), configDesc))

	layerDesc := ocispecs.Descriptor{
		MediaType: ocispecs.MediaTypeImageLayerGzip,
		Digest:    digest.FromBytes(layerBytes),
		Size:      int64(len(layerBytes)),
	}
	require.NoError(t, content.WriteBlob(ctx, buf, layerDesc.Digest.String(), bytes.NewReader(layerBytes), layerDesc))

	mfst := ocispecs.Manifest{
		Versioned: ocispecsgo.Versioned{SchemaVersion: 2},
		MediaType: ocispecs.MediaTypeImageManifest,
		Config:    configDesc,
		Layers:    []ocispecs.Descriptor{layerDesc},
	}
	dt, err := json.Marshal(mfst)
	require.NoError(t, err)
	desc := ocispecs.Descriptor{
		MediaType: ocispecs.MediaTypeImageManifest,
		Digest:    digest.FromBytes(dt),
		Size:      int64(len(dt)),
	}
	require.NoError(t, content.WriteBlob(ctx, buf, desc.Digest.String(), bytes.NewReader(dt), desc))
	return desc
}

func TestCompareArtifactStubbed(t *testing.T) {
	// Two identical in-memory content stores (structurally identical
	// manifest trees). CompareArtifact should return without error and
	// the resulting report should indicate no divergence.
	bufA := contentutil.NewBuffer()
	bufB := contentutil.NewBuffer()

	configBytes := []byte(`{"architecture":"amd64","os":"linux"}`)
	layerBytes := []byte("dummy-layer-content")

	descA := writeManifestTree(t, bufA, configBytes, layerBytes)
	descB := writeManifestTree(t, bufB, configBytes, layerBytes)

	// Both trees have the same bytes → same digests.
	require.Equal(t, descA.Digest, descB.Digest)

	subject := &Subject{Descriptor: descA, Provider: bufA}
	replay := &Subject{Descriptor: descB, Provider: bufB}

	report, err := CompareArtifact(context.Background(), subject, replay)
	require.NoError(t, err)
	require.NotNil(t, report)
	require.True(t, ReportMatched(report), "identical stores should report no divergence")
}

func TestCompareArtifactMismatch(t *testing.T) {
	bufA := contentutil.NewBuffer()
	bufB := contentutil.NewBuffer()

	descA := writeManifestTree(t, bufA, []byte(`{"os":"linux"}`), []byte("a"))
	descB := writeManifestTree(t, bufB, []byte(`{"os":"linux"}`), []byte("b"))
	require.NotEqual(t, descA.Digest, descB.Digest)

	subject := &Subject{Descriptor: descA, Provider: bufA}
	replay := &Subject{Descriptor: descB, Provider: bufB}

	report, err := CompareArtifact(context.Background(), subject, replay)
	require.NoError(t, err)
	require.NotNil(t, report)
	require.False(t, ReportMatched(report), "mismatching stores should produce divergence events")

	// JSON should round-trip.
	raw, err := ReportJSON(report)
	require.NoError(t, err)
	require.NotEmpty(t, raw)
	var parsed CompareReport
	require.NoError(t, json.Unmarshal(raw, &parsed))
}

func TestCompareSemanticNotImplemented(t *testing.T) {
	_, err := CompareSemantic(context.Background(), &Subject{}, &Subject{})
	require.Error(t, err)
	var nie *NotImplementedError
	require.ErrorAs(t, err, &nie)
	require.Equal(t, "--compare=semantic", nie.Feature)
}
