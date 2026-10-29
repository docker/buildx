package imagetools

import (
	"testing"

	"github.com/containerd/containerd/v2/core/images"
	"github.com/moby/buildkit/exporter/containerimage/exptypes"
	ocispecs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
)

func TestApplyAnnotations(t *testing.T) {
	t.Run("untyped defaults to index", func(t *testing.T) {
		annotations, err := applyAnnotations(nil, map[exptypes.AnnotationKey]string{
			{Key: "org.example.key"}: "value",
		}, ocispecs.MediaTypeImageIndex)
		require.NoError(t, err)
		require.Equal(t, map[string]string{"org.example.key": "value"}, annotations)
	})

	t.Run("manifest is unsupported", func(t *testing.T) {
		_, err := applyAnnotations(nil, map[exptypes.AnnotationKey]string{
			{Type: exptypes.AnnotationManifest, Key: "org.example.key"}: "value",
		}, ocispecs.MediaTypeImageIndex)
		require.EqualError(t, err, `manifest annotations are not supported by imagetools create because it does not modify manifests; use "index:" or "manifest-descriptor:" instead`)
	})

	t.Run("Docker manifest list rejects annotations", func(t *testing.T) {
		_, err := applyAnnotations(nil, map[exptypes.AnnotationKey]string{
			{Type: exptypes.AnnotationIndex, Key: "org.example.key"}: "value",
		}, images.MediaTypeDockerSchema2ManifestList)
		require.EqualError(t, err, `annotations are not supported for Docker manifest lists; use "oci-mediatypes=true" when building the source images`)
	})
}
