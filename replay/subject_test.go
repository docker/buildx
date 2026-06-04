package replay

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/containerd/containerd/v2/core/content"
	contentlocal "github.com/containerd/containerd/v2/plugins/content/local"
	"github.com/moby/buildkit/client/ociindex"
	"github.com/moby/buildkit/util/attestation"
	"github.com/opencontainers/go-digest"
	ocispecs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
)

// TestSubjectPredicateAcceptsSLSA02 asserts that a SLSA v0.2 attestation
// file is accepted and converted to the v1 shape used internally.
func TestSubjectPredicateAcceptsSLSA02(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "provenance.intoto.json")

	stmt := map[string]any{
		"_type":         "https://in-toto.io/Statement/v0.1",
		"predicateType": "https://slsa.dev/provenance/v0.2",
		"subject":       []any{},
		"predicate": map[string]any{
			"builder":   map[string]string{"id": "buildkit"},
			"buildType": "https://mobyproject.org/buildkit@v1",
			"invocation": map[string]any{
				"configSource": map[string]any{"uri": "https://example.com/dockerfile"},
				"parameters":   map[string]any{},
				"environment":  map[string]any{"platform": "linux/amd64"},
			},
		},
	}
	dt, err := json.Marshal(stmt)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, dt, 0644))

	subjects, err := LoadSubjects(context.Background(), nil, "", path)
	require.NoError(t, err)
	require.Len(t, subjects, 1)
	require.True(t, subjects[0].IsAttestationFile())

	_, err = subjects[0].Predicate(context.Background())
	require.NoError(t, err)
}

// TestSubjectPredicateRejectsUnknown asserts that a predicateType outside
// the SLSA v1 / v0.2 set is rejected with UnsupportedPredicateError.
func TestSubjectPredicateRejectsUnknown(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "provenance.intoto.json")

	stmt := map[string]any{
		"_type":         "https://in-toto.io/Statement/v0.1",
		"predicateType": "https://example.com/custom/v1",
		"subject":       []any{},
		"predicate":     map[string]any{},
	}
	dt, err := json.Marshal(stmt)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, dt, 0644))

	subjects, err := LoadSubjects(context.Background(), nil, "", path)
	require.NoError(t, err)
	_, err = subjects[0].Predicate(context.Background())
	require.Error(t, err)
	var unsup *UnsupportedPredicateError
	require.ErrorAs(t, err, &unsup)
	require.Equal(t, "https://example.com/custom/v1", unsup.PredicateType)
}

// TestSubjectPredicateAttestationFileSLSA1 asserts that an unsigned DSSE-less
// in-toto Statement carrying a SLSA v1 predicate round-trips through
// LoadSubjects + Predicate without error.
func TestSubjectPredicateAttestationFileSLSA1(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "provenance.intoto.json")
	stmt := map[string]any{
		"_type":         "https://in-toto.io/Statement/v0.1",
		"predicateType": "https://slsa.dev/provenance/v1",
		"subject":       []any{},
		"predicate": map[string]any{
			"buildDefinition": map[string]any{
				"externalParameters": map[string]any{
					"request": map[string]any{
						"frontend": "dockerfile.v0",
					},
				},
			},
		},
	}
	dt, err := json.Marshal(stmt)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, dt, 0644))

	subjects, err := LoadSubjects(context.Background(), nil, "", path)
	require.NoError(t, err)
	require.Len(t, subjects, 1)

	pred, err := subjects[0].Predicate(context.Background())
	require.NoError(t, err)
	require.Equal(t, "dockerfile.v0", pred.Frontend())
}

// TestSubjectPredicateRejectsSignedDSSE asserts that a DSSE envelope with
// non-empty signatures is rejected with SignatureVerificationRequiredError —
// replay never silently accepts a signed attestation in this slice.
func TestSubjectPredicateRejectsSignedDSSE(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "signed.dsse.json")

	inner := map[string]any{
		"_type":         "https://in-toto.io/Statement/v0.1",
		"predicateType": "https://slsa.dev/provenance/v1",
		"subject":       []any{},
		"predicate":     map[string]any{},
	}
	innerDt, err := json.Marshal(inner)
	require.NoError(t, err)

	env := map[string]any{
		"payload":     base64.StdEncoding.EncodeToString(innerDt),
		"payloadType": "application/vnd.in-toto+json",
		"signatures": []map[string]string{
			{"sig": "MEUCIQDstubbedsignaturebytes==", "keyid": "test-key"},
		},
	}
	dt, err := json.Marshal(env)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, dt, 0644))

	_, err = LoadSubjects(context.Background(), nil, "", path)
	require.Error(t, err)
	var sig *SignatureVerificationRequiredError
	require.ErrorAs(t, err, &sig)
	require.Equal(t, path, sig.Source)
	require.Equal(t, "dsse", sig.Envelope)
}

// TestSubjectPredicateRejectsSigstoreBundle asserts that a Sigstore bundle
// shape is rejected without signature verification support.
func TestSubjectPredicateRejectsSigstoreBundle(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bundle.sigstore.json")

	bundle := map[string]any{
		"mediaType": "application/vnd.dev.sigstore.bundle.v0.3+json",
		"verificationMaterial": map[string]any{
			"tlogEntries": []any{},
		},
		"dsseEnvelope": map[string]any{
			"payload":     base64.StdEncoding.EncodeToString([]byte(`{}`)),
			"payloadType": "application/vnd.in-toto+json",
			"signatures":  []any{},
		},
	}
	dt, err := json.Marshal(bundle)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, dt, 0644))

	_, err = LoadSubjects(context.Background(), nil, "", path)
	require.Error(t, err)
	var sig *SignatureVerificationRequiredError
	require.ErrorAs(t, err, &sig)
	require.Equal(t, "sigstore-bundle", sig.Envelope)
}

// TestSubjectPredicateAcceptsUnsignedDSSE asserts that a DSSE envelope with
// an empty (or missing) signatures array is still accepted — the rejection
// is gated on actual signatures being present.
func TestSubjectPredicateAcceptsUnsignedDSSE(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "unsigned.dsse.json")

	inner := map[string]any{
		"_type":         "https://in-toto.io/Statement/v0.1",
		"predicateType": "https://slsa.dev/provenance/v1",
		"subject":       []any{},
		"predicate":     map[string]any{},
	}
	innerDt, err := json.Marshal(inner)
	require.NoError(t, err)

	env := map[string]any{
		"payload":     base64.StdEncoding.EncodeToString(innerDt),
		"payloadType": "application/vnd.in-toto+json",
		"signatures":  []any{},
	}
	dt, err := json.Marshal(env)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, dt, 0644))

	subjects, err := LoadSubjects(context.Background(), nil, "", path)
	require.NoError(t, err)
	require.Len(t, subjects, 1)
	require.True(t, subjects[0].IsAttestationFile())
}

// TestLoadSubjectsIndexFanout builds an OCI layout with a two-platform
// image index (amd64 + arm64) and asserts LoadSubjects returns two subjects
// with distinct Descriptor.Platform.
func TestLoadSubjectsIndexFanout(t *testing.T) {
	dir := t.TempDir()

	store, err := contentlocal.NewStore(dir)
	require.NoError(t, err)

	ctx := context.Background()

	cfgAmd64 := []byte(`{"architecture":"amd64","os":"linux"}`)
	cfgAmd64Dgst, cfgAmd64Sz := putBlob(ctx, t, store, cfgAmd64, "application/vnd.oci.image.config.v1+json")
	cfgArm64 := []byte(`{"architecture":"arm64","os":"linux"}`)
	cfgArm64Dgst, cfgArm64Sz := putBlob(ctx, t, store, cfgArm64, "application/vnd.oci.image.config.v1+json")

	amd64Desc := putManifest(ctx, t, store, ocispecs.Manifest{
		MediaType: ocispecs.MediaTypeImageManifest,
		Config: ocispecs.Descriptor{
			MediaType: "application/vnd.oci.image.config.v1+json",
			Digest:    cfgAmd64Dgst,
			Size:      cfgAmd64Sz,
		},
	}, &ocispecs.Platform{Architecture: "amd64", OS: "linux"})

	arm64Desc := putManifest(ctx, t, store, ocispecs.Manifest{
		MediaType: ocispecs.MediaTypeImageManifest,
		Config: ocispecs.Descriptor{
			MediaType: "application/vnd.oci.image.config.v1+json",
			Digest:    cfgArm64Dgst,
			Size:      cfgArm64Sz,
		},
	}, &ocispecs.Platform{Architecture: "arm64", OS: "linux"})

	idx := ocispecs.Index{
		MediaType: ocispecs.MediaTypeImageIndex,
		Manifests: []ocispecs.Descriptor{amd64Desc, arm64Desc},
	}
	idx.SchemaVersion = 2

	idxDt, err := json.Marshal(idx)
	require.NoError(t, err)
	idxDgst, idxSize := putBlob(ctx, t, store, idxDt, ocispecs.MediaTypeImageIndex)

	storeIdx := ociindex.NewStoreIndex(dir)
	require.NoError(t, storeIdx.Put(ocispecs.Descriptor{
		MediaType: ocispecs.MediaTypeImageIndex,
		Digest:    idxDgst,
		Size:      idxSize,
	}, ociindex.Tag("latest")))

	subjects, err := LoadSubjects(ctx, nil, "", "oci-layout://"+dir+":latest")
	require.NoError(t, err)
	require.Len(t, subjects, 2, "expected two fan-out subjects")

	sort.Slice(subjects, func(i, j int) bool {
		return subjects[i].Descriptor.Platform.Architecture < subjects[j].Descriptor.Platform.Architecture
	})
	require.Equal(t, "amd64", subjects[0].Descriptor.Platform.Architecture)
	require.Equal(t, "arm64", subjects[1].Descriptor.Platform.Architecture)
	require.NotEqual(t, subjects[0].Descriptor.Digest, subjects[1].Descriptor.Digest)
}

// TestLoadSubjectsFanoutSkipsAttestation exercises the attestation-manifest
// filtering path: an index with an attestation manifest annotated via
// vnd.docker.reference.digest must not produce a bonus subject for the
// attestation.
func TestLoadSubjectsFanoutSkipsAttestation(t *testing.T) {
	dir := t.TempDir()
	store, err := contentlocal.NewStore(dir)
	require.NoError(t, err)
	ctx := context.Background()

	cfgDt := []byte(`{"architecture":"amd64","os":"linux"}`)
	cfgDgst, cfgSize := putBlob(ctx, t, store, cfgDt, "application/vnd.oci.image.config.v1+json")
	imgDesc := putManifest(ctx, t, store, ocispecs.Manifest{
		MediaType: ocispecs.MediaTypeImageManifest,
		Config: ocispecs.Descriptor{
			MediaType: "application/vnd.oci.image.config.v1+json",
			Digest:    cfgDgst,
			Size:      cfgSize,
		},
	}, &ocispecs.Platform{Architecture: "amd64", OS: "linux"})

	// Synthesize a bare "attestation manifest" that references imgDesc.
	attestManifest := ocispecs.Manifest{
		MediaType: ocispecs.MediaTypeImageManifest,
		Config: ocispecs.Descriptor{
			MediaType: "application/vnd.oci.image.config.v1+json",
			Digest:    cfgDgst,
			Size:      cfgSize,
		},
	}
	attestDt, err := json.Marshal(attestManifest)
	require.NoError(t, err)
	attestDgst, attestSize := putBlob(ctx, t, store, attestDt, ocispecs.MediaTypeImageManifest)

	attestDesc := ocispecs.Descriptor{
		MediaType: ocispecs.MediaTypeImageManifest,
		Digest:    attestDgst,
		Size:      attestSize,
		Annotations: map[string]string{
			attestation.DockerAnnotationReferenceDigest: imgDesc.Digest.String(),
		},
	}

	idx := ocispecs.Index{
		MediaType: ocispecs.MediaTypeImageIndex,
		Manifests: []ocispecs.Descriptor{imgDesc, attestDesc},
	}
	idx.SchemaVersion = 2
	idxDt, err := json.Marshal(idx)
	require.NoError(t, err)
	idxDgst, idxSize := putBlob(ctx, t, store, idxDt, ocispecs.MediaTypeImageIndex)

	storeIdx := ociindex.NewStoreIndex(dir)
	require.NoError(t, storeIdx.Put(ocispecs.Descriptor{
		MediaType: ocispecs.MediaTypeImageIndex,
		Digest:    idxDgst,
		Size:      idxSize,
	}, ociindex.Tag("latest")))

	subjects, err := LoadSubjects(ctx, nil, "", "oci-layout://"+dir+":latest")
	require.NoError(t, err)
	require.Len(t, subjects, 1, "attestation manifest should not expand to a subject")
	require.Equal(t, imgDesc.Digest, subjects[0].Descriptor.Digest)
	require.Equal(t, attestDgst, subjects[0].AttestationManifest().Digest, "subject should record its attestation manifest")
}

// putBlob writes raw bytes to the content store and returns the digest/size.
func putBlob(ctx context.Context, t *testing.T, store content.Ingester, dt []byte, mediaType string) (digest.Digest, int64) {
	t.Helper()
	dgst := digest.FromBytes(dt)
	desc := ocispecs.Descriptor{MediaType: mediaType, Digest: dgst, Size: int64(len(dt))}
	err := content.WriteBlob(ctx, store, dgst.String(), bytes.NewReader(dt), desc)
	require.NoError(t, err)
	return dgst, int64(len(dt))
}

// putManifest marshals an OCI manifest and writes it to the store. Returns
// the descriptor (with optional platform).
func putManifest(ctx context.Context, t *testing.T, store content.Ingester, mfst ocispecs.Manifest, plat *ocispecs.Platform) ocispecs.Descriptor {
	t.Helper()
	mfst.SchemaVersion = 2
	dt, err := json.Marshal(mfst)
	require.NoError(t, err)
	dgst, sz := putBlob(ctx, t, store, dt, ocispecs.MediaTypeImageManifest)
	return ocispecs.Descriptor{
		MediaType: ocispecs.MediaTypeImageManifest,
		Digest:    dgst,
		Size:      sz,
		Platform:  plat,
	}
}
