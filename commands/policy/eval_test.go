package policy

import (
	"testing"

	gwpb "github.com/moby/buildkit/frontend/gateway/pb"
	ocispecs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
)

func TestSourceResolverOptIncludesResolveAttestations(t *testing.T) {
	req := &gwpb.ResolveSourceMetaRequest{
		ResolveMode: "default",
		Image: &gwpb.ResolveSourceImageRequest{
			NoConfig:            true,
			ResolveAttestations: []string{"https://slsa.dev/provenance/v0.2"},
		},
	}
	platform := &ocispecs.Platform{OS: "linux", Architecture: "amd64"}

	opt := sourceResolverOpt(req, platform)
	require.NotNil(t, opt.ImageOpt)
	require.True(t, opt.ImageOpt.NoConfig)
	require.Equal(t, []string{"https://slsa.dev/provenance/v0.2"}, opt.ImageOpt.ResolveAttestations)
	require.Equal(t, "default", opt.ImageOpt.ResolveMode)
	require.Equal(t, platform, opt.ImageOpt.Platform)
}
