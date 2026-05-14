package replay

import (
	"testing"

	"github.com/docker/buildx/builder"
	"github.com/docker/buildx/util/buildflags"
	slsa1 "github.com/in-toto/in-toto-golang/in_toto/slsa_provenance/v1"
	provenancetypes "github.com/moby/buildkit/solver/llbsolver/provenance/types"
	ocispecs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
)

func TestCheckSecretsMissing(t *testing.T) {
	declared := []*provenancetypes.Secret{
		{ID: "required"},
		{ID: "optional", Optional: true},
	}
	err := CheckSecrets(declared, nil)
	require.Error(t, err)
	var mse *MissingSecretError
	require.ErrorAs(t, err, &mse)
	require.Equal(t, []string{"required"}, mse.IDs)
}

func TestCheckSecretsExtra(t *testing.T) {
	declared := []*provenancetypes.Secret{{ID: "a"}}
	provided := buildflags.Secrets{{ID: "a"}, {ID: "rogue"}}
	err := CheckSecrets(declared, provided)
	require.Error(t, err)
	var ese *ExtraSecretError
	require.ErrorAs(t, err, &ese)
	require.Equal(t, []string{"rogue"}, ese.IDs)
}

func TestCheckSecretsOptionalOmitted(t *testing.T) {
	declared := []*provenancetypes.Secret{
		{ID: "required"},
		{ID: "optional", Optional: true},
	}
	provided := buildflags.Secrets{{ID: "required"}}
	require.NoError(t, CheckSecrets(declared, provided))
}

func TestCheckSecretsOptionalProvidedAllowed(t *testing.T) {
	declared := []*provenancetypes.Secret{
		{ID: "required"},
		{ID: "optional", Optional: true},
	}
	provided := buildflags.Secrets{{ID: "required"}, {ID: "optional"}}
	require.NoError(t, CheckSecrets(declared, provided))
}

func TestCheckSSHMissing(t *testing.T) {
	declared := []*provenancetypes.SSH{{ID: "default"}}
	err := CheckSSH(declared, nil)
	var mse *MissingSSHError
	require.ErrorAs(t, err, &mse)
	require.Equal(t, []string{"default"}, mse.IDs)
}

func TestCheckSSHExtra(t *testing.T) {
	declared := []*provenancetypes.SSH{{ID: "default"}}
	provided := []*buildflags.SSH{{ID: "default"}, {ID: "rogue"}}
	err := CheckSSH(declared, provided)
	var ese *ExtraSSHError
	require.ErrorAs(t, err, &ese)
	require.Equal(t, []string{"rogue"}, ese.IDs)
}

func predicateWithAttrs(attrs map[string]string) *Predicate {
	// Ensure every fixture predicate carries a remote-source `context`
	// so BuildOptionsFromPredicate passes the replay context check.
	if attrs == nil {
		attrs = map[string]string{}
	}
	if _, ok := attrs["context"]; !ok {
		attrs["context"] = "https://github.com/docker/buildx.git"
	}
	pred := &Predicate{}
	pred.BuildDefinition.ExternalParameters.Request.Args = attrs
	return pred
}

func subjectWithPlatform(arch string) *Subject {
	return &Subject{
		Descriptor: ocispecs.Descriptor{
			Platform: &ocispecs.Platform{OS: "linux", Architecture: arch},
		},
	}
}

func TestBuildOptionsFromPredicate(t *testing.T) {
	s := subjectWithPlatform("amd64")

	pred := predicateWithAttrs(map[string]string{
		"target":                          "myapp",
		"filename":                        "Dockerfile.prod",
		"label:org.example.owner":         "alice",
		"build-arg:FOO":                   "bar",
		"build-arg:BUILDKIT_INLINE_CACHE": "1",
		"add-hosts":                       "foo:1.2.3.4,bar:5.6.7.8",
		"no-cache":                        "stage1,stage2",
		"context:app":                     "docker-image://alpine:3.18",
		"attest:provenance":               "mode=max",
		"attest:sbom":                     "true",
	})

	req := &BuildRequest{Mode: BuildModeFrontend}
	opt, err := BuildOptionsFromPredicate(s, pred, req)
	require.NoError(t, err)

	require.Equal(t, "myapp", opt.Target)
	require.Equal(t, "Dockerfile.prod", opt.Inputs.DockerfilePath)
	require.Equal(t, map[string]string{"org.example.owner": "alice"}, opt.Labels)
	require.Equal(t, map[string]string{"FOO": "bar", "BUILDKIT_INLINE_CACHE": "1"}, opt.BuildArgs)
	require.Equal(t, []string{"foo:1.2.3.4", "bar:5.6.7.8"}, opt.ExtraHosts)
	require.Equal(t, []string{"stage1", "stage2"}, opt.NoCacheFilter)
	require.False(t, opt.NoCache)

	require.Contains(t, opt.Inputs.NamedContexts, "app")
	require.Equal(t, "docker-image://alpine:3.18", opt.Inputs.NamedContexts["app"].Path)

	// Recorded attest:* attrs flow through to opt.Attests unchanged so the
	// replay output carries the same attestation shape as the original.
	require.Contains(t, opt.Attests, "provenance")
	require.NotNil(t, opt.Attests["provenance"])
	require.Equal(t, "mode=max", *opt.Attests["provenance"])
	require.Contains(t, opt.Attests, "sbom")
	require.Equal(t, "true", *opt.Attests["sbom"])

	// Platform should mirror the subject descriptor.
	require.Len(t, opt.Platforms, 1)
	require.Equal(t, "amd64", opt.Platforms[0].Architecture)

	// In frontend mode, no pin callback is attached.
	require.Empty(t, opt.Policy)
}

func TestBuildOptionsFromPredicateNoCacheAll(t *testing.T) {
	pred := predicateWithAttrs(map[string]string{
		"no-cache": "",
	})
	opt, err := BuildOptionsFromPredicate(subjectWithPlatform("amd64"), pred, &BuildRequest{Mode: BuildModeFrontend})
	require.NoError(t, err)
	require.True(t, opt.NoCache)
	require.Nil(t, opt.NoCacheFilter)
}

func TestBuildOptionsFromPredicateMaterialsModeAttachesPinCallback(t *testing.T) {
	pred := predicateWithAttrs(map[string]string{})
	opt, err := BuildOptionsFromPredicate(subjectWithPlatform("amd64"), pred, &BuildRequest{Mode: BuildModeMaterials})
	require.NoError(t, err)
	require.Len(t, opt.Policy, 1, "materials mode must attach strict pin callback")
	require.NotNil(t, opt.Policy[0].Callback, "Policy entry must carry a non-nil Callback")
	require.Empty(t, opt.Policy[0].Files, "replay pin entry must not reference policy files")
}

func TestBuildOptionsFromPredicateUsesConfigSourceAsPrimary(t *testing.T) {
	pred := predicateWithAttrs(map[string]string{
		"context":  "https://github.com/example/attrs.git",
		"filename": "Dockerfile.attrs",
		"target":   "myapp",
	})
	pred.BuildDefinition.ExternalParameters.ConfigSource.URI = "https://github.com/moby/buildkit.git#refs/tags/v0.29.0"
	pred.BuildDefinition.ExternalParameters.ConfigSource.Path = "Dockerfile"

	opt, err := BuildOptionsFromPredicate(subjectWithPlatform("amd64"), pred, &BuildRequest{Mode: BuildModeFrontend})
	require.NoError(t, err)
	require.Equal(t, "https://github.com/moby/buildkit.git#refs/tags/v0.29.0", opt.Inputs.ContextPath)
	require.Equal(t, "Dockerfile", opt.Inputs.DockerfilePath)
}

func TestBuildOptionsFromPredicateUsesConfigSourcePathForGatewayFrontend(t *testing.T) {
	pred := predicateWithAttrs(map[string]string{
		"source":  "docker/dockerfile-upstream:master",
		"cmdline": "docker/dockerfile-upstream:master",
	})
	pred.BuildDefinition.ExternalParameters.Request.Frontend = "gateway.v0"
	pred.BuildDefinition.ExternalParameters.ConfigSource.URI = "https://github.com/moby/buildkit.git#refs/tags/dockerfile/1.23.0"
	pred.BuildDefinition.ExternalParameters.ConfigSource.Path = "frontend/dockerfile/cmd/dockerfile-frontend/Dockerfile"

	opt, err := BuildOptionsFromPredicate(subjectWithPlatform("arm64"), pred, &BuildRequest{Mode: BuildModeMaterials})
	require.NoError(t, err)
	require.Equal(t, "https://github.com/moby/buildkit.git#refs/tags/dockerfile/1.23.0", opt.Inputs.ContextPath)
	require.Equal(t, "frontend/dockerfile/cmd/dockerfile-frontend/Dockerfile", opt.Inputs.DockerfilePath)
}

func TestMaterialsModePlatformWarningMismatch(t *testing.T) {
	pred := predicateWithAttrs(map[string]string{})
	pred.BuildDefinition.InternalParameters.BuilderPlatform = "linux/amd64"
	msg := materialsModePlatformWarning([]Target{{
		Subject:   subjectWithPlatform("amd64"),
		Predicate: pred,
	}}, []builder.Node{{
		Platforms: []ocispecs.Platform{{OS: "linux", Architecture: "arm64"}},
	}})
	require.Contains(t, msg, "provenance default platform linux/amd64")
	require.Contains(t, msg, "current builder instance default platform linux/arm64")
}

func TestMaterialsModePlatformWarningMatch(t *testing.T) {
	pred := predicateWithAttrs(map[string]string{})
	pred.BuildDefinition.InternalParameters.BuilderPlatform = "linux/amd64"
	msg := materialsModePlatformWarning([]Target{{
		Subject:   subjectWithPlatform("amd64"),
		Predicate: pred,
	}}, []builder.Node{{
		Platforms: []ocispecs.Platform{{OS: "linux", Architecture: "amd64"}},
	}})
	require.Empty(t, msg)
}

func TestMaterialsModePlatformWarningInferredFromMaterials(t *testing.T) {
	pred := predicateWithAttrs(map[string]string{})
	pred.BuildDefinition.ResolvedDependencies = []slsa1.ResourceDescriptor{
		{URI: "pkg:docker/golang@1.26-alpine3.23?platform=linux%2Famd64"},
		{URI: "pkg:docker/tonistiigi/xx@1.9.0?platform=linux%2Famd64"},
	}
	msg := materialsModePlatformWarning([]Target{{
		Subject:   subjectWithPlatform("arm64"),
		Predicate: pred,
	}}, []builder.Node{{
		Platforms: []ocispecs.Platform{{OS: "linux", Architecture: "arm64"}},
	}})
	require.Contains(t, msg, "provenance default platform linux/amd64")
	require.Contains(t, msg, "current builder instance default platform linux/arm64")
}

func TestMaterialsModePlatformWarningPrefersInferredDefaultPlatform(t *testing.T) {
	pred := predicateWithAttrs(map[string]string{})
	pred.BuildDefinition.InternalParameters.BuilderPlatform = "linux/arm64"
	pred.BuildDefinition.ResolvedDependencies = []slsa1.ResourceDescriptor{
		{URI: "pkg:docker/golang@1.26-alpine3.23?platform=linux%2Famd64"},
		{URI: "pkg:docker/tonistiigi/xx@1.9.0?platform=linux%2Famd64"},
	}
	msg := materialsModePlatformWarning([]Target{{
		Subject:   subjectWithPlatform("arm64"),
		Predicate: pred,
	}}, []builder.Node{{
		Platforms: []ocispecs.Platform{{OS: "linux", Architecture: "arm64"}},
	}})
	require.Contains(t, msg, "provenance default platform linux/amd64")
	require.Contains(t, msg, "current builder instance default platform linux/arm64")
}

func TestSubjectKeyWithPlatform(t *testing.T) {
	s := &Subject{Descriptor: ocispecs.Descriptor{
		Digest:   "sha256:deadbeef",
		Platform: &ocispecs.Platform{OS: "linux", Architecture: "arm64", Variant: "v8"},
	}}
	require.Equal(t, "sha256:deadbeef@linux/arm64/v8", SubjectKey(s))
}
