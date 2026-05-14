package replay

import (
	"encoding/json"

	"github.com/opencontainers/go-digest"
	ocispecsgo "github.com/opencontainers/image-spec/specs-go"
	ocispecs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/pkg/errors"
)

// Media types and artifact types that identify buildx snapshots and the
// materials manifest child.
const (
	// ArtifactTypeSnapshot is the artifactType for both the per-platform
	// snapshot index and the top-level multi-platform snapshot index.
	ArtifactTypeSnapshot = "application/vnd.docker.buildx.snapshots.v1+json"
	// ArtifactTypeMaterials is the artifactType for the materials artifact
	// manifest nested inside a per-platform snapshot index.
	ArtifactTypeMaterials = "application/vnd.docker.buildx.snapshots.materials.v1+json"

	// ociEmptyConfigMediaType is the OCI 1.1 empty descriptor for use as the
	// `config:` of an artifact manifest that carries no image configuration.
	// See the image-spec manifest guidance for the empty descriptor.
	ociEmptyConfigMediaType = "application/vnd.oci.empty.v1+json"
	// ociEmptyConfigDigest is the canonical sha256 of the two-byte `{}`
	// OCI empty config.
	ociEmptyConfigDigest = "sha256:44136fa355b3678a1146ad16f7e8649e94fb4fc21fe77e8310c060f61caaff8a"
	// ociEmptyConfigSize is the size in bytes of the OCI empty config (`{}`).
	ociEmptyConfigSize = 2

	// layerMediaTypeHTTP is the mediaType written for http material blob
	// layers (raw bytes served by an http/https material).
	layerMediaTypeHTTP = "application/octet-stream"
	// layerMediaTypeContainerBlob is the default mediaType used for a
	// container-blob layer material copied into the snapshot.
	layerMediaTypeContainerBlob = "application/vnd.oci.image.layer.v1.tar+gzip"
)

// OCIEmptyConfigDescriptor returns the descriptor buildx uses for the empty
// config on the materials artifact manifest. The two bytes of the empty
// config are inlined via the descriptor's `data` field (OCI 1.1) so a
// consumer never has to fetch the empty-config blob separately.
func OCIEmptyConfigDescriptor() ocispecs.Descriptor {
	return ocispecs.Descriptor{
		MediaType: ociEmptyConfigMediaType,
		Digest:    digest.Digest(ociEmptyConfigDigest),
		Size:      ociEmptyConfigSize,
		Data:      OCIEmptyConfigBytes(),
	}
}

// OCIEmptyConfigBytes returns the raw bytes of the OCI empty config (`{}`)
// that callers must write into the snapshot content store so the materials
// artifact manifest has a valid content-addressable config blob.
func OCIEmptyConfigBytes() []byte {
	return []byte("{}")
}

// MaterialsManifest builds the materials artifact manifest descriptor (the
// image-manifest document plus its serialized bytes and descriptor) from the
// ordered list of layer descriptors. The caller owns the task of copying the
// referenced layer bytes (and the empty config) into the snapshot store; this
// function produces only the manifest document and its addressable
// descriptor.
//
// The layers parameter is used verbatim and must already include (in order):
//
//  1. http material layers (mediaType application/octet-stream)
//  2. container-blob layers (mediaType vnd.oci.image.layer.v1.tar+gzip or
//     the recorded equivalent)
//  3. image-material root-index blobs kept opaque
//     (mediaType vnd.oci.image.index.v1+json)
func MaterialsManifest(layers []ocispecs.Descriptor) (ocispecs.Manifest, ocispecs.Descriptor, []byte, error) {
	if layers == nil {
		layers = []ocispecs.Descriptor{}
	}
	mfst := ocispecs.Manifest{
		Versioned:    ocispecsgo.Versioned{SchemaVersion: 2},
		MediaType:    ocispecs.MediaTypeImageManifest,
		ArtifactType: ArtifactTypeMaterials,
		Config:       OCIEmptyConfigDescriptor(),
		Layers:       layers,
	}
	dt, err := json.Marshal(mfst)
	if err != nil {
		return ocispecs.Manifest{}, ocispecs.Descriptor{}, nil, errors.Wrap(err, "marshal materials manifest")
	}
	desc := ocispecs.Descriptor{
		MediaType:    ocispecs.MediaTypeImageManifest,
		ArtifactType: ArtifactTypeMaterials,
		Digest:       digest.FromBytes(dt),
		Size:         int64(len(dt)),
	}
	return mfst, desc, dt, nil
}

// PerPlatformSnapshotIndex builds a per-platform snapshot index.
// `attestManifest` is the ORIGINAL provenance attestation manifest
// descriptor; it becomes the index's `subject` and its blob must be copied
// into the snapshot content store by the caller.
//
// `materialsManifestDesc` — descriptor of the materials artifact manifest —
// is included as the first entry in `manifests[]` when non-zero. Passing a
// zero descriptor omits the materials manifest entirely (used when
// `--include-materials=false`).
//
// `imageMaterialManifests` — platform-specific image manifests for each image
// material — follow the materials manifest. Each descriptor's `Digest`
// addresses the platform-specific manifest that the original build actually
// used; its chain (manifest + config + layers) is expected to be present in
// the snapshot store.
func PerPlatformSnapshotIndex(
	attestManifest ocispecs.Descriptor,
	materialsManifestDesc ocispecs.Descriptor,
	imageMaterialManifests []ocispecs.Descriptor,
) (ocispecs.Index, ocispecs.Descriptor, []byte, error) {
	manifests := make([]ocispecs.Descriptor, 0, 1+len(imageMaterialManifests))
	if materialsManifestDesc.Digest != "" {
		manifests = append(manifests, materialsManifestDesc)
	}
	manifests = append(manifests, imageMaterialManifests...)

	idx := ocispecs.Index{
		Versioned:    ocispecsgo.Versioned{SchemaVersion: 2},
		MediaType:    ocispecs.MediaTypeImageIndex,
		ArtifactType: ArtifactTypeSnapshot,
		Subject:      descriptorPtr(attestManifest),
		Manifests:    manifests,
	}
	dt, err := json.Marshal(idx)
	if err != nil {
		return ocispecs.Index{}, ocispecs.Descriptor{}, nil, errors.Wrap(err, "marshal per-platform snapshot index")
	}
	desc := ocispecs.Descriptor{
		MediaType:    ocispecs.MediaTypeImageIndex,
		ArtifactType: ArtifactTypeSnapshot,
		Digest:       digest.FromBytes(dt),
		Size:         int64(len(dt)),
	}
	return idx, desc, dt, nil
}

// MultiPlatformSnapshotIndex wraps N per-platform snapshot index descriptors
// into a top-level index. Each input descriptor must already carry its
// `Platform` field so consumers can pick the right child.
func MultiPlatformSnapshotIndex(perPlatform []ocispecs.Descriptor) (ocispecs.Index, ocispecs.Descriptor, []byte, error) {
	idx := ocispecs.Index{
		Versioned:    ocispecsgo.Versioned{SchemaVersion: 2},
		MediaType:    ocispecs.MediaTypeImageIndex,
		ArtifactType: ArtifactTypeSnapshot,
		Manifests:    perPlatform,
	}
	dt, err := json.Marshal(idx)
	if err != nil {
		return ocispecs.Index{}, ocispecs.Descriptor{}, nil, errors.Wrap(err, "marshal multi-platform snapshot index")
	}
	desc := ocispecs.Descriptor{
		MediaType:    ocispecs.MediaTypeImageIndex,
		ArtifactType: ArtifactTypeSnapshot,
		Digest:       digest.FromBytes(dt),
		Size:         int64(len(dt)),
	}
	return idx, desc, dt, nil
}

// descriptorPtr returns a pointer to desc, or nil when desc is the zero
// descriptor (so callers that pass no subject do not end up with an empty
// stub).
func descriptorPtr(desc ocispecs.Descriptor) *ocispecs.Descriptor {
	if desc.Digest == "" {
		return nil
	}
	out := desc
	return &out
}
