package pb

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

func TestResolvePaths(t *testing.T) {
	tmpwd, err := os.MkdirTemp("", "testresolvepaths")
	require.NoError(t, err)
	defer os.Remove(tmpwd)
	require.NoError(t, os.Chdir(tmpwd))
	tests := []struct {
		name    string
		options *BuildOptions
		want    *BuildOptions
	}{
		{
			name:    "contextpath",
			options: &BuildOptions{ContextPath: "test"},
			want:    &BuildOptions{ContextPath: filepath.Join(tmpwd, "test")},
		},
		{
			name:    "contextpath-cwd",
			options: &BuildOptions{ContextPath: "."},
			want:    &BuildOptions{ContextPath: tmpwd},
		},
		{
			name:    "contextpath-dash",
			options: &BuildOptions{ContextPath: "-"},
			want:    &BuildOptions{ContextPath: "-"},
		},
		{
			name:    "contextpath-ssh",
			options: &BuildOptions{ContextPath: "git@github.com:docker/buildx.git"},
			want:    &BuildOptions{ContextPath: "git@github.com:docker/buildx.git"},
		},
		{
			name:    "dockerfilename",
			options: &BuildOptions{DockerfileName: "test", ContextPath: "."},
			want:    &BuildOptions{DockerfileName: filepath.Join(tmpwd, "test"), ContextPath: tmpwd},
		},
		{
			name:    "dockerfilename-dash",
			options: &BuildOptions{DockerfileName: "-", ContextPath: "."},
			want:    &BuildOptions{DockerfileName: "-", ContextPath: tmpwd},
		},
		{
			name:    "dockerfilename-remote",
			options: &BuildOptions{DockerfileName: "test", ContextPath: "git@github.com:docker/buildx.git"},
			want:    &BuildOptions{DockerfileName: "test", ContextPath: "git@github.com:docker/buildx.git"},
		},
		{
			name: "contexts",
			options: &BuildOptions{NamedContexts: map[string]string{
				"a": "test1", "b": "test2",
				"alpine": "docker-image://alpine@sha256:0123456789", "project": "https://github.com/myuser/project.git",
			}},
			want: &BuildOptions{NamedContexts: map[string]string{
				"a": filepath.Join(tmpwd, "test1"), "b": filepath.Join(tmpwd, "test2"),
				"alpine": "docker-image://alpine@sha256:0123456789", "project": "https://github.com/myuser/project.git",
			}},
		},
		{
			name: "cache-from",
			options: &BuildOptions{
				CacheFrom: []*CacheOptionsEntry{
					{
						Type:  "local",
						Attrs: map[string]string{"src": "test"},
					},
					{
						Type:  "registry",
						Attrs: map[string]string{"ref": "user/app"},
					},
				},
			},
			want: &BuildOptions{
				CacheFrom: []*CacheOptionsEntry{
					{
						Type:  "local",
						Attrs: map[string]string{"src": filepath.Join(tmpwd, "test")},
					},
					{
						Type:  "registry",
						Attrs: map[string]string{"ref": "user/app"},
					},
				},
			},
		},
		{
			name: "cache-to",
			options: &BuildOptions{
				CacheTo: []*CacheOptionsEntry{
					{
						Type:  "local",
						Attrs: map[string]string{"dest": "test"},
					},
					{
						Type:  "registry",
						Attrs: map[string]string{"ref": "user/app"},
					},
				},
			},
			want: &BuildOptions{
				CacheTo: []*CacheOptionsEntry{
					{
						Type:  "local",
						Attrs: map[string]string{"dest": filepath.Join(tmpwd, "test")},
					},
					{
						Type:  "registry",
						Attrs: map[string]string{"ref": "user/app"},
					},
				},
			},
		},
		{
			name: "exports",
			options: &BuildOptions{
				Exports: []*ExportEntry{
					{
						Type:        "local",
						Destination: "-",
					},
					{
						Type:        "local",
						Destination: "test1",
					},
					{
						Type:        "tar",
						Destination: "test3",
					},
					{
						Type:        "oci",
						Destination: "-",
					},
					{
						Type:        "docker",
						Destination: "test4",
					},
					{
						Type:  "image",
						Attrs: map[string]string{"push": "true"},
					},
				},
			},
			want: &BuildOptions{
				Exports: []*ExportEntry{
					{
						Type:        "local",
						Destination: "-",
					},
					{
						Type:        "local",
						Destination: filepath.Join(tmpwd, "test1"),
					},
					{
						Type:        "tar",
						Destination: filepath.Join(tmpwd, "test3"),
					},
					{
						Type:        "oci",
						Destination: "-",
					},
					{
						Type:        "docker",
						Destination: filepath.Join(tmpwd, "test4"),
					},
					{
						Type:  "image",
						Attrs: map[string]string{"push": "true"},
					},
				},
			},
		},
		{
			name: "secrets",
			options: &BuildOptions{
				Secrets: []*Secret{
					{
						FilePath: "test1",
					},
					{
						ID:  "val",
						Env: "a",
					},
					{
						ID:       "test",
						FilePath: "test3",
					},
				},
			},
			want: &BuildOptions{
				Secrets: []*Secret{
					{
						FilePath: filepath.Join(tmpwd, "test1"),
					},
					{
						ID:  "val",
						Env: "a",
					},
					{
						ID:       "test",
						FilePath: filepath.Join(tmpwd, "test3"),
					},
				},
			},
		},
		{
			name: "ssh",
			options: &BuildOptions{
				SSH: []*SSH{
					{
						ID:    "default",
						Paths: []string{"test1", "test2"},
					},
					{
						ID:    "a",
						Paths: []string{"test3"},
					},
				},
			},
			want: &BuildOptions{
				SSH: []*SSH{
					{
						ID:    "default",
						Paths: []string{filepath.Join(tmpwd, "test1"), filepath.Join(tmpwd, "test2")},
					},
					{
						ID:    "a",
						Paths: []string{filepath.Join(tmpwd, "test3")},
					},
				},
			},
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveOptionPaths(tt.options)
			require.NoError(t, err)
			if !proto.Equal(tt.want, got) {
				t.Fatalf("expected %#v, got %#v", tt.want, got)
			}
		})
	}
}
