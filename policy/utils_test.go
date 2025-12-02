package policy

import (
	"testing"

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

		// multiple separators → cut before second one
		{"git.tag.author", "git.tag"},
		{"git.tag.author.email", "git.tag"},
		{"git.tag[0][1]", "git.tag"},
		{"git.tag[0]", "git.tag"},

		{"a.b.c", "a.b"},
	}

	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			require.Equal(t, tt.out, trimKey(tt.in))
		})
	}
}
