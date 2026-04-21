package replay

import (
	"testing"

	slsa "github.com/in-toto/in-toto-golang/in_toto/slsa_provenance/common"
	slsa1 "github.com/in-toto/in-toto-golang/in_toto/slsa_provenance/v1"
	provenancetypes "github.com/moby/buildkit/solver/llbsolver/provenance/types"
	"github.com/stretchr/testify/require"
)

func TestPredicateMethods(t *testing.T) {
	raw := provenancetypes.ProvenancePredicateSLSA1{
		BuildDefinition: provenancetypes.ProvenanceBuildDefinitionSLSA1{
			ExternalParameters: provenancetypes.ProvenanceExternalParametersSLSA1{
				ConfigSource: provenancetypes.ProvenanceConfigSourceSLSA1{
					URI:    "https://example.com/build",
					Digest: slsa.DigestSet{"sha256": "deadbeef"},
					Path:   "Dockerfile",
				},
				Request: provenancetypes.Parameters{
					Frontend: "gateway.v0",
					Args: map[string]string{
						"target":            "release",
						"build-arg:FOO":     "bar",
						"label:maintainer":  "me",
						"attest:sbom":       "generator=scanner",
						"attest:provenance": "mode=max",
					},
					Secrets: []*provenancetypes.Secret{{ID: "github_token"}, {ID: "npm_token", Optional: true}},
					SSH:     []*provenancetypes.SSH{{ID: "default"}},
					Locals:  []*provenancetypes.LocalSource{{Name: "context"}},
				},
			},
			InternalParameters: provenancetypes.ProvenanceInternalParametersSLSA1{
				BuildConfig: &provenancetypes.BuildConfig{
					Definition: []provenancetypes.BuildStep{{ID: "step-0"}},
				},
			},
			ProvenanceBuildDefinition: slsa1.ProvenanceBuildDefinition{
				ResolvedDependencies: []slsa1.ResourceDescriptor{
					{URI: "docker-image://alpine:latest", Digest: slsa.DigestSet{"sha256": "aaaa"}},
					{URI: "https://example.com/pkg.tar.gz", Digest: slsa.DigestSet{"sha256": "bbbb"}},
				},
			},
		},
	}
	pred := (*Predicate)(&raw)

	t.Run("Frontend returns recorded frontend", func(t *testing.T) {
		require.Equal(t, "gateway.v0", pred.Frontend())
	})

	t.Run("Frontend falls back to dockerfile.v0", func(t *testing.T) {
		empty := &Predicate{}
		require.Equal(t, defaultFrontend, empty.Frontend())
	})

	t.Run("FrontendAttrs strips attestation attrs", func(t *testing.T) {
		attrs := pred.FrontendAttrs()
		require.Equal(t, "release", attrs["target"])
		require.Equal(t, "bar", attrs["build-arg:FOO"])
		require.Equal(t, "me", attrs["label:maintainer"])
		_, hasSBOM := attrs["attest:sbom"]
		require.False(t, hasSBOM, "attest:sbom should be filtered out")
		_, hasProv := attrs["attest:provenance"]
		require.False(t, hasProv, "attest:provenance should be filtered out")
	})

	t.Run("FrontendAttrs returns fresh map", func(t *testing.T) {
		attrs := pred.FrontendAttrs()
		attrs["injected"] = "yes"
		// The predicate's underlying map should be untouched.
		require.NotContains(t, pred.BuildDefinition.ExternalParameters.Request.Args, "injected")
	})

	t.Run("ConfigSource", func(t *testing.T) {
		cs := pred.ConfigSource()
		require.Equal(t, "https://example.com/build", cs.URI)
		require.Equal(t, "Dockerfile", cs.Path)
	})

	t.Run("Secrets", func(t *testing.T) {
		secrets := pred.Secrets()
		require.Len(t, secrets, 2)
		require.Equal(t, "github_token", secrets[0].ID)
		require.False(t, secrets[0].Optional)
		require.True(t, secrets[1].Optional)
	})

	t.Run("SSH", func(t *testing.T) {
		ssh := pred.SSH()
		require.Len(t, ssh, 1)
		require.Equal(t, "default", ssh[0].ID)
	})

	t.Run("Locals", func(t *testing.T) {
		locals := pred.Locals()
		require.Len(t, locals, 1)
		require.Equal(t, "context", locals[0].Name)
	})

	t.Run("ResolvedDependencies", func(t *testing.T) {
		deps := pred.ResolvedDependencies()
		require.Len(t, deps, 2)
		require.Equal(t, "docker-image://alpine:latest", deps[0].URI)
	})

	t.Run("HasBuildDefinition true", func(t *testing.T) {
		require.True(t, pred.HasBuildDefinition())
	})

	t.Run("HasBuildDefinition false when empty", func(t *testing.T) {
		empty := &Predicate{}
		require.False(t, empty.HasBuildDefinition())
	})

	t.Run("HasBuildDefinition false when BuildConfig has no steps", func(t *testing.T) {
		p := &Predicate{}
		p.BuildDefinition.InternalParameters.BuildConfig = &provenancetypes.BuildConfig{}
		require.False(t, p.HasBuildDefinition())
	})
}
