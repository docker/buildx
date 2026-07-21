package driver

import (
	"context"
	"testing"

	"github.com/docker/buildx/policy"
	"github.com/moby/buildkit/client"
	digest "github.com/opencontainers/go-digest"
	ocispecs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
)

type nopSubLogger struct{}

func (nopSubLogger) Wrap(_ string, fn func() error) error { return fn() }
func (nopSubLogger) Log(int, []byte)                      {}
func (nopSubLogger) SetStatus(*client.VertexStatus)       {}

func TestVerifyImageRefTaggedCanonical(t *testing.T) {
	dgst := digest.FromString("buildkit")
	ref := "moby/buildkit:v0.31.2@" + dgst.String()
	var verifiedRef string

	pinned, applied, err := VerifyImageRef(context.Background(), nopSubLogger{}, ref, nil, nil, func(_ context.Context, ref string, _ *ocispecs.Platform, _ policy.SourceMetadataResolver) (digest.Digest, error) {
		verifiedRef = ref
		return dgst, nil
	})
	require.NoError(t, err)
	require.True(t, applied)
	require.Equal(t, "docker.io/"+ref, verifiedRef)
	require.Equal(t, "docker.io/"+ref, pinned)
}

func TestVerifyImageRefDigestOnly(t *testing.T) {
	dgst := digest.FromString("buildkit")
	called := false

	pinned, applied, err := VerifyImageRef(context.Background(), nopSubLogger{}, "moby/buildkit@"+dgst.String(), nil, nil, func(_ context.Context, _ string, _ *ocispecs.Platform, _ policy.SourceMetadataResolver) (digest.Digest, error) {
		called = true
		return dgst, nil
	})
	require.NoError(t, err)
	require.False(t, applied)
	require.False(t, called)
	require.Equal(t, "moby/buildkit@"+dgst.String(), pinned)
}
