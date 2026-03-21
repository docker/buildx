package policy

import (
	"testing"

	policytypes "github.com/docker/buildx/policy"
	"github.com/docker/buildx/util/sourcemeta"
	gwpb "github.com/moby/buildkit/frontend/gateway/pb"
	"github.com/moby/buildkit/solver/pb"
	ocispecs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
)

func TestParsePlatform(t *testing.T) {
	t.Run("normalize", func(t *testing.T) {
		platform, err := parsePlatform("linux/arm/v7")
		require.NoError(t, err)
		require.Equal(t, &ocispecs.Platform{
			OS:           "linux",
			Architecture: "arm",
			Variant:      "v7",
		}, platform)
	})

	t.Run("invalid", func(t *testing.T) {
		platform, err := parsePlatform("not-a-platform")
		require.Nil(t, platform)
		require.Error(t, err)
		require.ErrorContains(t, err, "invalid platform \"not-a-platform\"")
		require.ErrorContains(t, err, "unknown operating system or architecture")
	})
}

func TestToPBPlatform(t *testing.T) {
	platform := ocispecs.Platform{OS: "linux", Architecture: "amd64"}
	require.Equal(t, &pb.Platform{
		OS:           "linux",
		Architecture: "amd64",
	}, toPBPlatform(platform))
}

func TestSourceResolverOptIncludesResolveAttestations(t *testing.T) {
	req := &gwpb.ResolveSourceMetaRequest{
		ResolveMode: "default",
		Image: &gwpb.ResolveSourceImageRequest{
			NoConfig:            true,
			ResolveAttestations: []string{"https://slsa.dev/provenance/v0.2"},
		},
	}
	platform := &ocispecs.Platform{OS: "linux", Architecture: "amd64"}

	opt := sourcemeta.ToResolverOpt(req, platform)
	require.NotNil(t, opt.ImageOpt)
	require.True(t, opt.ImageOpt.NoConfig)
	require.Equal(t, []string{"https://slsa.dev/provenance/v0.2"}, opt.ImageOpt.ResolveAttestations)
	require.Equal(t, "default", opt.ImageOpt.ResolveMode)
	require.Equal(t, platform, opt.ImageOpt.Platform)
}

func TestSanitizePrintInputClearsDepthRecursively(t *testing.T) {
	inp := policytypes.Input{
		Env: policytypes.Env{Depth: 7, Filename: "Dockerfile"},
		Image: &policytypes.Image{
			Provenance: &policytypes.ImageProvenance{
				Materials: []policytypes.Input{
					{
						Env: policytypes.Env{Depth: 3, Target: "app"},
						Image: &policytypes.Image{
							Provenance: &policytypes.ImageProvenance{
								Materials: []policytypes.Input{
									{Env: policytypes.Env{Depth: 2}},
								},
							},
						},
					},
				},
			},
		},
	}

	sanitizePrintInput(&inp)

	require.Zero(t, inp.Env.Depth)
	require.Equal(t, "Dockerfile", inp.Env.Filename)
	require.Zero(t, inp.Image.Provenance.Materials[0].Env.Depth)
	require.Equal(t, "app", inp.Image.Provenance.Materials[0].Env.Target)
	require.Zero(t, inp.Image.Provenance.Materials[0].Image.Provenance.Materials[0].Env.Depth)
}

func TestSelectReloadFields(t *testing.T) {
	unknowns := []string{
		"image.provenance",
		"image.provenance.materials[0].image.hasProvenance",
		"git.tag",
	}

	t.Run("exact match", func(t *testing.T) {
		reload, invalid := selectReloadFields([]string{"git.tag"}, unknowns)
		require.Equal(t, []string{"git.tag"}, reload)
		require.Nil(t, invalid)
	})

	t.Run("ancestor mapping", func(t *testing.T) {
		reload, invalid := selectReloadFields([]string{"image.provenance.materials[0].image.labels"}, unknowns)
		require.ElementsMatch(t, []string{"image.provenance"}, reload)
		require.Nil(t, invalid)
	})

	t.Run("dedupe mapped reloads", func(t *testing.T) {
		reload, invalid := selectReloadFields([]string{
			"image.provenance.materials[0].image.labels",
			"image.provenance.materials[0].image.user",
		}, unknowns)
		require.ElementsMatch(t, []string{
			"image.provenance",
		}, reload)
		require.Nil(t, invalid)
	})

	t.Run("invalid fields reported", func(t *testing.T) {
		reload, invalid := selectReloadFields([]string{"image.labels", "foo.bar"}, unknowns)
		require.Empty(t, reload)
		require.Equal(t, []string{"image.labels", "foo.bar"}, invalid)
	})

	t.Run("mix exact mapped invalid", func(t *testing.T) {
		reload, invalid := selectReloadFields([]string{
			"git.tag",
			"image.provenance.materials[0].image.env",
			"no.such.field",
		}, unknowns)
		require.ElementsMatch(t, []string{"git.tag", "image.provenance"}, reload)
		require.Equal(t, []string{"no.such.field"}, invalid)
	})

	t.Run("nested material prerequisites", func(t *testing.T) {
		reload, invalid := selectReloadFields([]string{
			"image.provenance.materials[0].image.provenance.materials[1].image.labels",
		}, unknowns)
		require.ElementsMatch(t, []string{
			"image.provenance",
		}, reload)
		require.Nil(t, invalid)
	})

	t.Run("material field after provenance loaded", func(t *testing.T) {
		reload, invalid := selectReloadFields([]string{
			"image.provenance.materials[0].image.hasProvenance",
		}, []string{
			"image.provenance.materials[0].image.hasProvenance",
		})
		require.ElementsMatch(t, []string{
			"image.provenance.materials[0].image.hasProvenance",
		}, reload)
		require.Nil(t, invalid)
	})
}

func TestFilterInvalidFields(t *testing.T) {
	out := filterInvalidFields([]string{
		"git.tag",
		"image.checksum",
	}, map[string]struct{}{
		"git.tag": {},
	})
	require.Equal(t, []string{"image.checksum"}, out)

	out = filterInvalidFields([]string{"foo.bar"}, nil)
	require.Equal(t, []string{"foo.bar"}, out)
}

func TestMaterialFieldPrerequisites(t *testing.T) {
	t.Run("non material field", func(t *testing.T) {
		prereq, ok := materialFieldPrerequisites("image.provenance")
		require.False(t, ok)
		require.Nil(t, prereq)
	})

	t.Run("single level material field", func(t *testing.T) {
		prereq, ok := materialFieldPrerequisites("image.provenance.materials[0].image.labels")
		require.True(t, ok)
		require.Equal(t, []string{"image.provenance"}, prereq)
	})

	t.Run("nested material field", func(t *testing.T) {
		prereq, ok := materialFieldPrerequisites("image.provenance.materials[0].image.provenance.materials[1].image.labels")
		require.True(t, ok)
		require.Equal(t, []string{
			"image.provenance",
			"image.provenance.materials[0].image.provenance",
		}, prereq)
	})
}
