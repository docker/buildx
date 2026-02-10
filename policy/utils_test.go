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
			data.foo.bar == 1
		}
	`)
	require.NoError(t, err)

	all := collectUnknowns([]*ast.Module{mod}, nil)
	require.ElementsMatch(t, []string{"git.tag", "image.signatures"}, all)

	filtered := collectUnknowns([]*ast.Module{mod}, []string{"input.image.signatures"})
	require.Equal(t, []string{"image.signatures"}, filtered)
}

func TestRuntimeUnknownInputRefs(t *testing.T) {
	require.Nil(t, runtimeUnknownInputRefs(nil))
	require.Nil(t, runtimeUnknownInputRefs(&state{}))

	st := &state{
		Unknowns: map[string]struct{}{
			funcVerifyGitSignature: {},
		},
	}
	require.Equal(t, []string{"git.commit"}, runtimeUnknownInputRefs(st))
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
