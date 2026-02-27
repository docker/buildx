package policy

import (
	"testing"

	"github.com/open-policy-agent/opa/v1/ast"
	"github.com/stretchr/testify/require"
)

func TestTrimKey(t *testing.T) {
	tests := []struct {
		in  string
		out string
	}{
		// no separators
		{"git", "git"},
		{"foo", "foo"},

		// one separator → stays as-is
		{"git.tag", "git.tag"},
		{"git[tag", "git[tag"},
		{"input.git.tag", "git.tag"},
		{"input.git[tag", "git[tag"},

		// multiple separators → cut before second one
		{"git.tag.author", "git.tag"},
		{"git.tag.author.email", "git.tag"},
		{"git.tag[0][1]", "git.tag"},
		{"git.tag[0]", "git.tag"},
		{"input.git.tag.author", "git.tag"},
		{"input.git.tag[0]", "git.tag"},
		{"input.image.provenance.materials[0].image.hasProvenance", "image.provenance.materials[0].image.hasProvenance"},
		{"image.provenance.materials[0].image.labels", "image.provenance.materials[0].image.labels"},
		{"input.image.provenance.materials[0].image.provenance.predicateType", "image.provenance.materials[0].image.provenance.predicateType"},
		{"input.image.provenance.materials[0].image.signatures[0].signer.certificateIssuer", "image.provenance.materials[0].image.signatures[0].signer.certificateIssuer"},
		{"input.image.provenance.materials[10].image.hasProvenance", "image.provenance.materials[10].image.hasProvenance"},

		{"a.b.c", "a.b"},
	}

	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			require.Equal(t, tt.out, trimKey(tt.in))
		})
	}
}

func TestCollectUnknowns(t *testing.T) {
	mod, err := ast.ParseModule("x.rego", `
		package x
		p if {
			input.git.tag[0].author == "a"
			input.image.signatures[_].signer.certificateIssuer != ""
			input.image.provenance.materials[0].image.hasProvenance
			data.foo.bar == 1
		}
	`)
	require.NoError(t, err)

	all := collectUnknowns([]*ast.Module{mod}, nil)
	require.ElementsMatch(t, []string{"git.tag", "image.signatures", "image.provenance.materials[0].image.hasProvenance"}, all)

	filtered := collectUnknowns([]*ast.Module{mod}, []string{"input.image.signatures", "input.image.provenance.materials[0].image.hasProvenance"})
	require.ElementsMatch(t, []string{"image.signatures", "image.provenance.materials[0].image.hasProvenance"}, filtered)
}

func TestCollectUnknownsParentAllowedMatchesChildRef(t *testing.T) {
	mod, err := ast.ParseModule("x.rego", `
		package x
		p if {
			input.image.provenance.materials[0].image.provenance.predicateType != ""
			input.image.provenance.materials[0].image.signatures[0].signer.certificateIssuer != ""
			input.image.provenance.materials[0].git.tag.name != ""
			input.foo.bar != ""
			input.image.provenance.materials[10].image.hasProvenance
		}
	`)
	require.NoError(t, err)

	filtered := collectUnknowns([]*ast.Module{mod}, []string{
		"input.image.provenance.materials[0].image.provenance",
		"input.image.provenance.materials[0].image.signatures",
		"input.image.provenance.materials[0].git.tag",
		"input.foo.b",
		"input.image.provenance.materials[1].image",
	})

	require.ElementsMatch(t, []string{
		"image.provenance.materials[0].image.provenance",
		"image.provenance.materials[0].image.signatures",
		"image.provenance.materials[0].git.tag",
	}, filtered)
}

func TestMatchAllowedOrParentBoundary(t *testing.T) {
	allowed := map[string]struct{}{
		"foo.b":                               {},
		"image.provenance.materials[1].image": {},
	}

	_, ok := matchAllowedOrParent("foo.bar", allowed)
	require.False(t, ok)

	_, ok = matchAllowedOrParent("image.provenance.materials[10].image.hasProvenance", allowed)
	require.False(t, ok)
}

func TestRuntimeUnknownInputRefs(t *testing.T) {
	require.Nil(t, runtimeUnknownInputRefs(nil))
	require.Nil(t, runtimeUnknownInputRefs(&state{}))

	st := &state{
		Unknowns: map[string]struct{}{
			funcVerifyGitSignature:     {},
			funcArtifactAttestation:    {},
			funcGithubAttestation:      {},
			funcVerifyHTTPPGPSignature: {},
		},
	}
	require.Equal(t, []string{"git.commit", "http.checksum"}, runtimeUnknownInputRefs(st))
}

func TestMissingInputRefsWithRuntimeUnknowns(t *testing.T) {
	mod, err := ast.ParseModule("x.rego", `
		package x
		p if {
			input.git.ref != ""
		}
	`)
	require.NoError(t, err)

	in := &Input{
		Git: &Git{
			Ref: "refs/heads/main",
		},
	}
	missing := missingInputRefs([]*ast.Module{mod}, in, []string{"git.commit"})
	require.Equal(t, []string{"git.commit"}, missing)
}
