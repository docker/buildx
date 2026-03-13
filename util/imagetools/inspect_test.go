package imagetools

import (
	"context"
	"testing"

	"github.com/containerd/containerd/v2/core/images"
	"github.com/containerd/containerd/v2/core/remotes"
	"github.com/moby/buildkit/client/ociindex"
	"github.com/opencontainers/go-digest"
	ocispecs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
)

func TestFetchReferrersOCILayoutArtifactTypeFilter(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	idx := ociindex.NewStoreIndex(dir)
	subject := digest.FromString("subject")

	attestation := ocispecs.Descriptor{
		MediaType:    ocispecs.MediaTypeImageManifest,
		ArtifactType: artifactTypeAttestationManifest,
		Digest:       digest.FromString("attestation"),
		Size:         123,
		Annotations: map[string]string{
			images.AnnotationManifestSubject: subject.String(),
		},
	}
	require.NoError(t, idx.Put(attestation))

	signature := ocispecs.Descriptor{
		MediaType:    ocispecs.MediaTypeImageManifest,
		ArtifactType: "application/vnd.dev.sigstore.bundle.v0.3+json",
		Digest:       digest.FromString("signature"),
		Size:         456,
		Annotations: map[string]string{
			images.AnnotationManifestSubject: subject.String(),
		},
	}
	require.NoError(t, idx.Put(signature))

	loc, err := ParseLocation("oci-layout://" + dir + ":latest")
	require.NoError(t, err)

	r := New(Opt{})
	refs, err := r.FetchReferrers(context.Background(), loc, subject, remotes.WithReferrerArtifactTypes(artifactTypeAttestationManifest))
	require.NoError(t, err)
	require.Len(t, refs, 1)
	require.Equal(t, attestation.Digest, refs[0].Digest)
	require.Equal(t, attestation.ArtifactType, refs[0].ArtifactType)
}
