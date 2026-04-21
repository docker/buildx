package replay

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/containerd/containerd/v2/core/content"
	"github.com/docker/buildx/util/buildflags"
	"github.com/opencontainers/go-digest"
	ocispecs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
)

func writeBlob(t *testing.T, root string, dt []byte) digest.Digest {
	t.Helper()
	d := digest.FromBytes(dt)
	dir := filepath.Join(root, "blobs", d.Algorithm().String())
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, d.Encoded()), dt, 0o644))
	return d
}

// seedOCILayout creates a minimum OCI layout skeleton: a `oci-layout` marker
// file plus the blobs/sha256 dir. The caller writes blobs on top.
func seedOCILayout(t *testing.T, root string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "blobs", "sha256"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "oci-layout"), []byte(`{"imageLayoutVersion":"1.0.0"}`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "index.json"), []byte(`{"schemaVersion":2,"manifests":[]}`), 0o644))
}

func TestMaterialsResolverProvenanceDefault(t *testing.T) {
	r, err := NewMaterialsResolver(nil)
	require.NoError(t, err)
	require.True(t, r.Sentinel())
	require.False(t, r.HasStores())
}

func TestMaterialsResolverProvenanceSentinelExplicit(t *testing.T) {
	r, err := NewMaterialsResolver([]string{"provenance"})
	require.NoError(t, err)
	require.True(t, r.Sentinel())
}

func TestMaterialsResolverOCILayoutDigestLookup(t *testing.T) {
	dir := t.TempDir()
	seedOCILayout(t, dir)
	payload := []byte(`{"mediaType":"application/vnd.oci.image.manifest.v1+json"}`)
	d := writeBlob(t, dir, payload)

	r, err := NewMaterialsResolver([]string{"oci-layout://" + dir})
	require.NoError(t, err)
	require.False(t, r.Sentinel())
	require.True(t, r.HasStores())

	desc, provider, err := r.Resolve(context.Background(), "pkg:docker/alpine@3.18", d)
	require.NoError(t, err)
	require.Equal(t, d, desc.Digest)
	require.NotNil(t, provider)

	// Verify that the returned provider serves the blob.
	dt, err := content.ReadBlob(context.Background(), provider, ocispecs.Descriptor{Digest: d, Size: int64(len(payload))})
	require.NoError(t, err)
	require.Equal(t, payload, dt)
}

func TestMaterialsResolverOCILayoutMiss(t *testing.T) {
	dir := t.TempDir()
	seedOCILayout(t, dir)

	r, err := NewMaterialsResolver([]string{"oci-layout://" + dir})
	require.NoError(t, err)

	absent := digest.FromBytes([]byte("nope"))
	_, _, err = r.Resolve(context.Background(), "pkg:docker/missing@1.0", absent)
	require.Error(t, err)
	var mnf *MaterialNotFoundError
	require.ErrorAs(t, err, &mnf)
	require.Equal(t, absent.String(), mnf.Digest)
}

func TestMaterialsResolverOverridesByURI(t *testing.T) {
	dir := t.TempDir()
	seedOCILayout(t, dir)
	payload := []byte("hello-override")
	d := writeBlob(t, dir, payload)

	uri := "https://example.com/whatever.tar"
	specs := []string{uri + "=oci-layout://" + dir}
	r, err := NewMaterialsResolver(specs)
	require.NoError(t, err)

	overrides := r.Overrides()
	require.Contains(t, overrides, uri)

	desc, provider, err := r.Resolve(context.Background(), uri, d)
	require.NoError(t, err)
	require.Equal(t, d, desc.Digest)
	require.NotNil(t, provider)
}

func TestMaterialsResolverOverridesByDigest(t *testing.T) {
	dir := t.TempDir()
	seedOCILayout(t, dir)
	payload := []byte("bytes-for-digest-override")
	d := writeBlob(t, dir, payload)

	specs := []string{d.String() + "=oci-layout://" + dir}
	r, err := NewMaterialsResolver(specs)
	require.NoError(t, err)

	desc, provider, err := r.Resolve(context.Background(), "", d)
	require.NoError(t, err)
	require.Equal(t, d, desc.Digest)
	require.NotNil(t, provider)
}

func TestMaterialsResolverOverrideFile(t *testing.T) {
	dir := t.TempDir()
	payload := []byte("standalone-file-override")
	path := filepath.Join(dir, "payload.bin")
	require.NoError(t, os.WriteFile(path, payload, 0o644))

	d := digest.FromBytes(payload)
	uri := "https://example.com/a.tar"
	r, err := NewMaterialsResolver([]string{uri + "=" + path})
	require.NoError(t, err)

	desc, provider, err := r.Resolve(context.Background(), uri, d)
	require.NoError(t, err)
	require.Equal(t, d, desc.Digest)
	require.Equal(t, int64(len(payload)), desc.Size)
	require.NotNil(t, provider)

	dt, err := content.ReadBlob(context.Background(), provider, desc)
	require.NoError(t, err)
	require.Equal(t, payload, dt)
}

func TestMaterialsResolverMalformedOverride(t *testing.T) {
	_, err := NewMaterialsResolver([]string{"=oci-layout:///foo"})
	require.Error(t, err)
}

func TestMaterialsResolverRegistryNotImplemented(t *testing.T) {
	_, err := NewMaterialsResolver([]string{"registry://my/ref"})
	require.Error(t, err)
	var nie *NotImplementedError
	require.ErrorAs(t, err, &nie)
}

func TestMaterialsResolverUnknownSpec(t *testing.T) {
	_, err := NewMaterialsResolver([]string{"gopher://x"})
	require.Error(t, err)
}

func TestMaterialsResolverOverrideIsNotConfusedWithPath(t *testing.T) {
	// An absolute path that happens to contain '=' characters must still be
	// recognised as a path, not as an override.
	dir := t.TempDir()
	oddDir := filepath.Join(dir, "has=equals")
	require.NoError(t, os.MkdirAll(filepath.Join(oddDir, "blobs", "sha256"), 0o755))

	r, err := NewMaterialsResolver([]string{oddDir})
	require.NoError(t, err)
	require.True(t, r.HasStores())
}

// TestMaterialsResolverSnapshotBackedLookup builds a snapshot via Snapshot(),
// then points a fresh MaterialsResolver at the resulting OCI layout and
// resolves both an http and (synthetic) image material. The snapshot-backed
// lookup path (materials.go lookupSnapshot) is exercised.
func TestMaterialsResolverSnapshotBackedLookup(t *testing.T) {
	fx := makeSnapshotFixture(t)
	dest := t.TempDir()
	exp := buildflags.ExportEntry{Type: "oci", Destination: dest, Attrs: map[string]string{"tar": "false"}}
	req := &SnapshotRequest{
		Targets:          []Target{{Subject: fx.subject, Predicate: fx.predicate}},
		IncludeMaterials: true,
		Materials:        snapshotOverrideResolver(t, fx.httpURI, fx.httpBytes),
		Output:           &exp,
	}
	require.NoError(t, Snapshot(context.Background(), nil, "", req))

	layout := "oci-layout://" + dest
	r, err := NewMaterialsResolver([]string{layout})
	require.NoError(t, err)

	// 1. Http material: resolves by digest into the materials manifest's
	//    layer set.
	desc, provider, err := r.Resolve(context.Background(), fx.httpURI, fx.httpDigest)
	require.NoError(t, err)
	require.NotNil(t, provider)
	require.Equal(t, fx.httpDigest, desc.Digest)
	got, err := content.ReadBlob(context.Background(), provider, desc)
	require.NoError(t, err)
	require.Equal(t, fx.httpBytes, got)

	// 2. The subject manifest itself is reachable by its digest: Snapshot
	//    does not store it in the materials manifest, but the per-platform
	//    snapshot index's `manifests[]` references the attestation-manifest
	//    chain which CopyChain has copied into the layout. A lookup by the
	//    attestation manifest's digest must resolve (direct content-by-
	//    digest path).
	attestDesc, attestProvider, err := r.Resolve(context.Background(), "", fx.attestDigest)
	require.NoError(t, err)
	require.NotNil(t, attestProvider)
	require.Equal(t, fx.attestDigest, attestDesc.Digest)
}
