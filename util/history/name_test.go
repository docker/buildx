package history

import (
	"testing"

	"github.com/docker/buildx/localstate"
	"github.com/stretchr/testify/require"
)

func TestBuildName(t *testing.T) {
	tests := []struct {
		name  string
		attrs map[string]string
		state *localstate.State
		want  string
	}{
		{
			name: "empty",
			want: "",
		},
		{
			name: "override",
			attrs: map[string]string{
				"build-arg:BUILDKIT_BUILD_NAME": "foobar",
			},
			want: "foobar",
		},
		{
			name: "local dockerfile path",
			state: &localstate.State{
				LocalPath:      "/tmp/project",
				DockerfilePath: "/tmp/project/deploy/Dockerfile.release",
			},
			want: "project/deploy/Dockerfile.release",
		},
		{
			name: "local default dockerfile",
			state: &localstate.State{
				LocalPath:      "/tmp/project",
				DockerfilePath: "/tmp/project/Dockerfile",
			},
			want: "project",
		},
		{
			name: "git query input",
			attrs: map[string]string{
				"input:context": "https://github.com/docker/buildx.git?ref=main",
			},
			want: "https://github.com/docker/buildx.git#main",
		},
		{
			name: "vcs source",
			attrs: map[string]string{
				"vcs:source": "https://github.com/docker/buildx.git?ref=main",
			},
			want: "https://github.com/docker/buildx.git#main",
		},
		{
			name: "vcs local context fallback",
			attrs: map[string]string{
				"vcs:localdir:context": "subdir",
			},
			want: "subdir",
		},
		{
			name: "dockerfile attrs",
			attrs: map[string]string{
				"filename":                "Dockerfile.release",
				"vcs:localdir:dockerfile": "deploy",
			},
			want: "deploy/Dockerfile.release",
		},
		{
			name: "target only",
			attrs: map[string]string{
				"target": "release",
			},
			want: "release",
		},
		{
			name: "context overrides local name",
			attrs: map[string]string{
				"context": "subdir",
				"target":  "release",
			},
			state: &localstate.State{
				LocalPath:      "/tmp/project",
				DockerfilePath: "/tmp/project/deploy/Dockerfile.release",
			},
			want: "subdir (release)",
		},
		{
			name: "remote local path ignored",
			attrs: map[string]string{
				"filename":                "Dockerfile.release",
				"vcs:localdir:dockerfile": "deploy",
			},
			state: &localstate.State{
				LocalPath:      "https://github.com/docker/buildx.git",
				DockerfilePath: "/tmp/project/ignored/Dockerfile",
			},
			want: "deploy/Dockerfile.release",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, BuildName(tt.attrs, tt.state))
		})
	}
}
