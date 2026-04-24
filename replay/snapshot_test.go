package replay

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/containerd/containerd/v2/core/content"
	"github.com/docker/buildx/util/buildflags"
	slsa1 "github.com/in-toto/in-toto-golang/in_toto/slsa_provenance/v1"
	"github.com/moby/buildkit/client/ociindex"
	provenancetypes "github.com/moby/buildkit/solver/llbsolver/provenance/types"
	"github.com/moby/buildkit/util/contentutil"
	digest "github.com/opencontainers/go-digest"
	ocispecsgo "github.com/opencontainers/image-spec/specs-go"
	ocispecs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
)

// snapshotFixture produces a Subject + Predicate pair backed by an in-memory
// content provider. The subject carries a synthetic image manifest; the
// attestation manifest references an in-toto SLSA v1 provenance statement
// whose ResolvedDependencies name one http material.
//
// Returned slice of material bytes gives tests a handle on the http material
// payload so they can be pinned via a MaterialsResolver override.
type snapshotFixture struct {
	subject      *Subject
	predicate    *Predicate
	httpBytes    []byte
	httpDigest   digest.Digest
	httpURI      string
	attestDigest digest.Digest
	subjectDesc  ocispecs.Descriptor
	attestDesc   ocispecs.Descriptor
	provider     contentutil.Buffer
}

func makeSnapshotFixture(t *testing.T) *snapshotFixture {
	t.Helper()
	ctx := context.Background()

	buf := contentutil.NewBuffer()

	// 1. Synthetic subject image: config + single layer + manifest.
	configBytes := []byte(`{"architecture":"amd64","os":"linux","rootfs":{"type":"layers","diff_ids":[]}}`)
	configDesc := writeBlobBuf(ctx, t, buf, ocispecs.MediaTypeImageConfig, configBytes)
	layerBytes := []byte("fake-image-layer")
	layerDesc := writeBlobBuf(ctx, t, buf, ocispecs.MediaTypeImageLayerGzip, layerBytes)

	subjectManifest := ocispecs.Manifest{
		Versioned: ocispecsgo.Versioned{SchemaVersion: 2},
		MediaType: ocispecs.MediaTypeImageManifest,
		Config:    configDesc,
		Layers:    []ocispecs.Descriptor{layerDesc},
	}
	subjectBytes, err := json.Marshal(subjectManifest)
	require.NoError(t, err)
	subjectDesc := writeBlobBuf(ctx, t, buf, ocispecs.MediaTypeImageManifest, subjectBytes)
	subjectDesc.Platform = &ocispecs.Platform{OS: "linux", Architecture: "amd64"}

	// 2. http material (reachable later by digest).
	httpBytes := []byte("hello-from-http-material")
	httpDigest := digest.FromBytes(httpBytes)
	_ = writeBlobBuf(ctx, t, buf, "application/octet-stream", httpBytes)

	httpURI := "https://example.com/payload.tgz"

	// 3. SLSA v1 provenance predicate.
	pred := provenancetypes.ProvenancePredicateSLSA1{}
	pred.BuildDefinition.ExternalParameters.Request.Frontend = "dockerfile.v0"
	pred.BuildDefinition.ResolvedDependencies = []slsa1.ResourceDescriptor{
		{
			URI: httpURI,
			Digest: map[string]string{
				"sha256": httpDigest.Encoded(),
			},
		},
	}

	// 4. in-toto Statement wrapping the predicate.
	predBytes, err := json.Marshal(pred)
	require.NoError(t, err)
	stmt := map[string]any{
		"_type":         "https://in-toto.io/Statement/v1",
		"predicateType": "https://slsa.dev/provenance/v1",
		"subject": []map[string]any{
			{
				"name":   "synthetic",
				"digest": map[string]string{"sha256": subjectDesc.Digest.Encoded()},
			},
		},
		"predicate": json.RawMessage(predBytes),
	}
	stmtBytes, err := json.Marshal(stmt)
	require.NoError(t, err)

	stmtDesc := writeBlobBufWithAnnotations(
		ctx, t, buf,
		"application/vnd.in-toto+json",
		stmtBytes,
		map[string]string{"in-toto.io/predicate-type": "https://slsa.dev/provenance/v1"},
	)

	// 5. Attestation manifest referencing the in-toto statement layer.
	attestManifest := ocispecs.Manifest{
		Versioned:    ocispecsgo.Versioned{SchemaVersion: 2},
		MediaType:    ocispecs.MediaTypeImageManifest,
		ArtifactType: "application/vnd.in-toto+json",
		Config:       OCIEmptyConfigDescriptor(),
		Layers:       []ocispecs.Descriptor{stmtDesc},
	}
	// Write empty config (required by the attestation manifest chain).
	_ = writeBlobBufRaw(ctx, t, buf, OCIEmptyConfigDescriptor(), OCIEmptyConfigBytes())

	attestBytes, err := json.Marshal(attestManifest)
	require.NoError(t, err)
	attestDesc := writeBlobBuf(ctx, t, buf, ocispecs.MediaTypeImageManifest, attestBytes)
	attestDesc.ArtifactType = "application/vnd.in-toto+json"

	// 6. Build the Subject pointing at the image manifest.
	s := &Subject{
		Descriptor:     subjectDesc,
		Provider:       buf,
		inputRef:       "synthetic://subject",
		kind:           subjectKindImage,
		attestManifest: attestDesc,
	}
	p := Predicate(pred)

	return &snapshotFixture{
		subject:      s,
		predicate:    &p,
		httpBytes:    httpBytes,
		httpDigest:   httpDigest,
		httpURI:      httpURI,
		attestDigest: attestDesc.Digest,
		subjectDesc:  subjectDesc,
		attestDesc:   attestDesc,
		provider:     buf,
	}
}

// writeBlobBuf stores dt in buf under a mediaType and returns the descriptor.
func writeBlobBuf(ctx context.Context, t *testing.T, buf contentutil.Buffer, mediaType string, dt []byte) ocispecs.Descriptor {
	t.Helper()
	d := digest.FromBytes(dt)
	desc := ocispecs.Descriptor{MediaType: mediaType, Digest: d, Size: int64(len(dt))}
	require.NoError(t, content.WriteBlob(ctx, buf, d.String(), bytes.NewReader(dt), desc))
	return desc
}

func writeBlobBufWithAnnotations(ctx context.Context, t *testing.T, buf contentutil.Buffer, mediaType string, dt []byte, ann map[string]string) ocispecs.Descriptor {
	desc := writeBlobBuf(ctx, t, buf, mediaType, dt)
	desc.Annotations = ann
	return desc
}

func writeBlobBufRaw(ctx context.Context, t *testing.T, buf contentutil.Buffer, desc ocispecs.Descriptor, dt []byte) ocispecs.Descriptor {
	t.Helper()
	require.NoError(t, content.WriteBlob(ctx, buf, desc.Digest.String(), bytes.NewReader(dt), desc))
	return desc
}

// snapshotOverrideResolver builds a MaterialsResolver whose single override
// points at a temp file carrying the material bytes. That lets Snapshot
// resolve the material without any network or registry access.
func snapshotOverrideResolver(t *testing.T, uri string, dt []byte) *MaterialsResolver {
	t.Helper()
	tmp := filepath.Join(t.TempDir(), "override.bin")
	require.NoError(t, os.WriteFile(tmp, dt, 0o644))
	r, err := NewMaterialsResolver([]string{uri + "=" + tmp})
	require.NoError(t, err)
	return r
}

func TestSnapshotWritesOCILayout(t *testing.T) {
	fx := makeSnapshotFixture(t)

	dest := t.TempDir()
	req := &SnapshotRequest{
		Targets:          []Target{{Subject: fx.subject, Predicate: fx.predicate}},
		IncludeMaterials: true,
		Materials:        snapshotOverrideResolver(t, fx.httpURI, fx.httpBytes),
		Output: &buildflags.ExportEntry{
			Type:        "oci",
			Destination: dest,
			Attrs:       map[string]string{"tar": "false"},
		},
	}
	require.NoError(t, Snapshot(context.Background(), nil, "", req))

	// OCI layout skeleton present.
	_, err := os.Stat(filepath.Join(dest, "oci-layout"))
	require.NoError(t, err)
	_, err = os.Stat(filepath.Join(dest, "index.json"))
	require.NoError(t, err)
	_, err = os.Stat(filepath.Join(dest, "blobs", "sha256"))
	require.NoError(t, err)

	// Index.json points at the per-platform snapshot index (single-platform
	// case — Snapshot does not wrap in a multi-platform index).
	idx, err := ociindex.NewStoreIndex(dest).Read()
	require.NoError(t, err)
	require.Len(t, idx.Manifests, 1)
	root := idx.Manifests[0]
	require.Equal(t, ArtifactTypeSnapshot, root.ArtifactType)

	// Walk the root and assert its subject is the attestation manifest.
	rootBlobPath := filepath.Join(dest, "blobs", root.Digest.Algorithm().String(), root.Digest.Encoded())
	rootData, err := os.ReadFile(rootBlobPath)
	require.NoError(t, err)
	var ppIdx ocispecs.Index
	require.NoError(t, json.Unmarshal(rootData, &ppIdx))
	require.Equal(t, ArtifactTypeSnapshot, ppIdx.ArtifactType)
	require.NotNil(t, ppIdx.Subject)
	require.Equal(t, fx.attestDigest, ppIdx.Subject.Digest)

	// First manifests[] entry must be the materials artifact manifest.
	require.NotEmpty(t, ppIdx.Manifests)
	matDesc := ppIdx.Manifests[0]
	require.Equal(t, ArtifactTypeMaterials, matDesc.ArtifactType)

	// The http material layer lives inside the materials manifest.
	matPath := filepath.Join(dest, "blobs", matDesc.Digest.Algorithm().String(), matDesc.Digest.Encoded())
	matRaw, err := os.ReadFile(matPath)
	require.NoError(t, err)
	var mm ocispecs.Manifest
	require.NoError(t, json.Unmarshal(matRaw, &mm))
	require.Equal(t, ArtifactTypeMaterials, mm.ArtifactType)

	var gotHTTPLayer bool
	for _, l := range mm.Layers {
		if l.Digest == fx.httpDigest {
			gotHTTPLayer = true
			// Blob on disk must equal the pinned material bytes.
			lp := filepath.Join(dest, "blobs", l.Digest.Algorithm().String(), l.Digest.Encoded())
			got, err := os.ReadFile(lp)
			require.NoError(t, err)
			require.Equal(t, fx.httpBytes, got)
		}
	}
	require.True(t, gotHTTPLayer, "materials manifest must include the http material layer")

	// Round-trip: a MaterialsResolver pointed at oci-layout://<dest> must
	// find the http material through the snapshot-backed lookup path.
	layout := fmt.Sprintf("oci-layout://%s", dest)
	rr, err := NewMaterialsResolver([]string{layout})
	require.NoError(t, err)
	desc, provider, err := rr.Resolve(context.Background(), fx.httpURI, fx.httpDigest)
	require.NoError(t, err)
	require.NotNil(t, provider)
	require.Equal(t, fx.httpDigest, desc.Digest)

	gotBytes, err := content.ReadBlob(context.Background(), provider, desc)
	require.NoError(t, err)
	require.Equal(t, fx.httpBytes, gotBytes)
}

func TestSnapshotRejectsAttestationFileSubject(t *testing.T) {
	fx := makeSnapshotFixture(t)
	fx.subject.kind = subjectKindAttestationFile

	req := &SnapshotRequest{
		Targets: []Target{{Subject: fx.subject, Predicate: fx.predicate}},
		Output: &buildflags.ExportEntry{
			Type:        "oci",
			Destination: t.TempDir(),
			Attrs:       map[string]string{"tar": "false"},
		},
	}
	err := Snapshot(context.Background(), nil, "", req)
	require.Error(t, err)
	var unsup *UnsupportedSubjectError
	require.ErrorAs(t, err, &unsup)
}

func TestSnapshotMissingOutputRejected(t *testing.T) {
	fx := makeSnapshotFixture(t)

	err := Snapshot(context.Background(), nil, "", &SnapshotRequest{
		Targets: []Target{{Subject: fx.subject, Predicate: fx.predicate}},
	})
	require.Error(t, err)
}
