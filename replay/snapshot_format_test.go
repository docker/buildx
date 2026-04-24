package replay

import (
	"encoding/json"
	"testing"

	digest "github.com/opencontainers/go-digest"
	ocispecs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
)

// Round-trip tests for the snapshot-format writers. Each test builds a
// concrete document through the writer, serializes it back through json,
// and asserts the expected shape.

func TestPerPlatformSnapshotIndex_WithMaterials(t *testing.T) {
	// Fake attestation manifest descriptor (subject chain into the snapshot).
	attest := ocispecs.Descriptor{
		MediaType:    ocispecs.MediaTypeImageManifest,
		ArtifactType: "application/vnd.in-toto+json",
		Digest:       digest.FromBytes([]byte("attest")),
		Size:         12,
	}

	// Two materials layers: one http blob, one image-material root index.
	httpBytes := []byte("http-material-bytes")
	rootBytes := []byte("root-index-bytes")
	layers := []ocispecs.Descriptor{
		{
			MediaType: "application/octet-stream",
			Digest:    digest.FromBytes(httpBytes),
			Size:      int64(len(httpBytes)),
		},
		{
			MediaType: ocispecs.MediaTypeImageIndex,
			Digest:    digest.FromBytes(rootBytes),
			Size:      int64(len(rootBytes)),
		},
	}
	_, matDesc, matData, err := MaterialsManifest(layers)
	require.NoError(t, err)
	require.Equal(t, ArtifactTypeMaterials, matDesc.ArtifactType)
	require.Equal(t, ocispecs.MediaTypeImageManifest, matDesc.MediaType)
	require.NotZero(t, matDesc.Size)
	require.Equal(t, digest.FromBytes(matData), matDesc.Digest)

	// Parse the materials manifest and assert §5.2.2 rules.
	var mm ocispecs.Manifest
	require.NoError(t, json.Unmarshal(matData, &mm))
	require.Equal(t, 2, mm.SchemaVersion)
	require.Equal(t, ocispecs.MediaTypeImageManifest, mm.MediaType)
	require.Equal(t, ArtifactTypeMaterials, mm.ArtifactType)
	require.Equal(t, OCIEmptyConfigDescriptor().Digest, mm.Config.Digest)
	require.Equal(t, OCIEmptyConfigDescriptor().MediaType, mm.Config.MediaType)
	require.Equal(t, int64(2), mm.Config.Size)
	require.Len(t, mm.Layers, 2)
	require.Equal(t, layers[0].Digest, mm.Layers[0].Digest)
	require.Equal(t, layers[1].Digest, mm.Layers[1].Digest)
	require.Nil(t, mm.Subject, "materials manifest MUST NOT carry a subject (§5.2.2)")

	// Build the per-platform snapshot index on top.
	imgMfst := ocispecs.Descriptor{
		MediaType: ocispecs.MediaTypeImageManifest,
		Digest:    digest.FromBytes([]byte("platform-manifest")),
		Size:      42,
		Platform:  &ocispecs.Platform{Architecture: "amd64", OS: "linux"},
	}
	_, ppDesc, ppData, err := PerPlatformSnapshotIndex(attest, matDesc, []ocispecs.Descriptor{imgMfst})
	require.NoError(t, err)
	require.Equal(t, ArtifactTypeSnapshot, ppDesc.ArtifactType)
	require.Equal(t, ocispecs.MediaTypeImageIndex, ppDesc.MediaType)
	require.Equal(t, digest.FromBytes(ppData), ppDesc.Digest)

	var pp ocispecs.Index
	require.NoError(t, json.Unmarshal(ppData, &pp))
	require.Equal(t, 2, pp.SchemaVersion)
	require.Equal(t, ocispecs.MediaTypeImageIndex, pp.MediaType)
	require.Equal(t, ArtifactTypeSnapshot, pp.ArtifactType)

	require.NotNil(t, pp.Subject, "per-platform snapshot index MUST carry a subject (§5.2.1)")
	require.Equal(t, attest.Digest, pp.Subject.Digest)
	require.Equal(t, attest.MediaType, pp.Subject.MediaType)
	require.Equal(t, attest.Size, pp.Subject.Size)

	require.Len(t, pp.Manifests, 2)
	// First entry MUST be the materials manifest when it is present.
	require.Equal(t, matDesc.Digest, pp.Manifests[0].Digest)
	require.Equal(t, ArtifactTypeMaterials, pp.Manifests[0].ArtifactType)
	// Remaining entries are the per-material platform manifests.
	require.Equal(t, imgMfst.Digest, pp.Manifests[1].Digest)
	require.NotNil(t, pp.Manifests[1].Platform)
	require.Equal(t, "amd64", pp.Manifests[1].Platform.Architecture)

	// Snapshot indexes are deterministic — no creation timestamp annotation.
	require.Empty(t, pp.Annotations[ocispecs.AnnotationCreated])
}

func TestPerPlatformSnapshotIndex_WithoutMaterials(t *testing.T) {
	// When --include-materials=false the materials manifest is omitted:
	// the caller passes a zero descriptor.
	attest := ocispecs.Descriptor{
		MediaType: ocispecs.MediaTypeImageManifest,
		Digest:    digest.FromBytes([]byte("attest-noop")),
		Size:      10,
	}
	imgMfst := ocispecs.Descriptor{
		MediaType: ocispecs.MediaTypeImageManifest,
		Digest:    digest.FromBytes([]byte("platform-manifest-noop")),
		Size:      11,
	}

	_, _, ppData, err := PerPlatformSnapshotIndex(attest, ocispecs.Descriptor{}, []ocispecs.Descriptor{imgMfst})
	require.NoError(t, err)

	var pp ocispecs.Index
	require.NoError(t, json.Unmarshal(ppData, &pp))
	require.Len(t, pp.Manifests, 1, "zero materials descriptor should be dropped")
	require.Equal(t, imgMfst.Digest, pp.Manifests[0].Digest)
	require.NotEqual(t, ArtifactTypeMaterials, pp.Manifests[0].ArtifactType)
}

func TestMultiPlatformSnapshotIndex(t *testing.T) {
	// Build two dummy per-platform snapshot descriptors with populated
	// Platform fields; wrap via MultiPlatformSnapshotIndex.
	amd := ocispecs.Descriptor{
		MediaType:    ocispecs.MediaTypeImageIndex,
		ArtifactType: ArtifactTypeSnapshot,
		Digest:       digest.FromBytes([]byte("pp-amd64")),
		Size:         100,
		Platform:     &ocispecs.Platform{Architecture: "amd64", OS: "linux"},
	}
	arm := ocispecs.Descriptor{
		MediaType:    ocispecs.MediaTypeImageIndex,
		ArtifactType: ArtifactTypeSnapshot,
		Digest:       digest.FromBytes([]byte("pp-arm64")),
		Size:         110,
		Platform:     &ocispecs.Platform{Architecture: "arm64", OS: "linux"},
	}
	_, desc, data, err := MultiPlatformSnapshotIndex([]ocispecs.Descriptor{amd, arm})
	require.NoError(t, err)
	require.Equal(t, ArtifactTypeSnapshot, desc.ArtifactType)
	require.Equal(t, ocispecs.MediaTypeImageIndex, desc.MediaType)
	require.Equal(t, digest.FromBytes(data), desc.Digest)

	var idx ocispecs.Index
	require.NoError(t, json.Unmarshal(data, &idx))
	require.Equal(t, 2, idx.SchemaVersion)
	require.Equal(t, ocispecs.MediaTypeImageIndex, idx.MediaType)
	require.Equal(t, ArtifactTypeSnapshot, idx.ArtifactType)
	require.Nil(t, idx.Subject, "top-level multi-platform snapshot index MUST NOT carry a subject (§5.2.3)")
	require.Len(t, idx.Manifests, 2)

	for i, child := range idx.Manifests {
		require.Equal(t, ArtifactTypeSnapshot, child.ArtifactType, "child %d", i)
		require.Equal(t, ocispecs.MediaTypeImageIndex, child.MediaType, "child %d", i)
		require.NotNil(t, child.Platform, "child %d must carry platform", i)
	}
	require.Equal(t, "amd64", idx.Manifests[0].Platform.Architecture)
	require.Equal(t, "arm64", idx.Manifests[1].Platform.Architecture)
	require.Empty(t, idx.Annotations[ocispecs.AnnotationCreated])
}

func TestOCIEmptyConfig(t *testing.T) {
	// Round-trip the empty config constants: digest of the bytes must equal
	// the exported descriptor digest.
	bytes := OCIEmptyConfigBytes()
	require.Equal(t, "{}", string(bytes))
	require.Equal(t, digest.FromBytes(bytes).String(), string(OCIEmptyConfigDescriptor().Digest))
	require.Equal(t, int64(len(bytes)), OCIEmptyConfigDescriptor().Size)
}
