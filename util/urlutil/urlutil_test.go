package urlutil

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIsHTTPURL(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{
			name:  "https url",
			input: "https://example.com/repo.git",
			want:  true,
		},
		{
			name:  "http url",
			input: "http://example.com/repo.git",
			want:  true,
		},
		{
			name:  "http prefix only",
			input: "http://",
			want:  true,
		},
		{
			name:  "non-http protocol",
			input: "git://example.com/repo.git",
			want:  false,
		},
		{
			name:  "no protocol",
			input: "example.com/repo.git",
			want:  false,
		},
		{
			name:  "uppercase protocol is not matched",
			input: "HTTPS://example.com/repo.git",
			want:  false,
		},
		{
			name:  "leading whitespace does not match",
			input: " https://example.com/repo.git",
			want:  false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, IsHTTPURL(tc.input))
		})
	}
}

func TestIsRemoteURL(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{
			name:  "https url is remote",
			input: "https://example.com/not-a-git-url",
			want:  true,
		},
		{
			name:  "http url is remote",
			input: "http://example.com/path",
			want:  true,
		},
		{
			name:  "scp style git remote",
			input: "git@github.com:moby/buildkit.git",
			want:  true,
		},
		{
			name:  "github shorthand git remote",
			input: "github.com/moby/buildkit",
			want:  true,
		},
		{
			name:  "relative local path is not remote",
			input: "./hack",
			want:  false,
		},
		{
			name:  "plain local path is not remote",
			input: "hack/dockerfiles",
			want:  false,
		},
		{
			name:  "unknown protocol is not remote",
			input: "docker-image://alpine",
			want:  false,
		},
		{
			name:  "empty is not remote",
			input: "",
			want:  false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, IsRemoteURL(tc.input))
		})
	}
}
