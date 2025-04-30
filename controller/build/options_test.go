package build

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/docker/buildx/controller/pb"
	"github.com/stretchr/testify/require"
)

func TestResolvePaths(t *testing.T) {
	tmpwd, err := os.MkdirTemp("", "testresolvepaths")
	require.NoError(t, err)
	defer os.Remove(tmpwd)
	require.NoError(t, os.Chdir(tmpwd))
	tests := []struct {
		name    string
		options *Options
		want    *Options
	}{
		{
			name:    "contextpath",
			options: &Options{ContextPath: "test"},
			want:    &Options{ContextPath: filepath.Join(tmpwd, "test")},
		},
		{
			name:    "contextpath-cwd",
			options: &Options{ContextPath: "."},
			want:    &Options{ContextPath: tmpwd},
		},
		{
			name:    "contextpath-dash",
			options: &Options{ContextPath: "-"},
			want:    &Options{ContextPath: "-"},
		},
		{
			name:    "contextpath-ssh",
			options: &Options{ContextPath: "git@github.com:docker/buildx.git"},
			want:    &Options{ContextPath: "git@github.com:docker/buildx.git"},
		},
		{
			name:    "dockerfilename",
			options: &Options{DockerfileName: "test", ContextPath: "."},
			want:    &Options{DockerfileName: filepath.Join(tmpwd, "test"), ContextPath: tmpwd},
		},
		{
			name:    "dockerfilename-dash",
			options: &Options{DockerfileName: "-", ContextPath: "."},
			want:    &Options{DockerfileName: "-", ContextPath: tmpwd},
		},
		{
			name:    "dockerfilename-remote",
			options: &Options{DockerfileName: "test", ContextPath: "git@github.com:docker/buildx.git"},
			want:    &Options{DockerfileName: "test", ContextPath: "git@github.com:docker/buildx.git"},
		},
		{
			name: "contexts",
			options: &Options{NamedContexts: map[string]string{
				"a": "test1", "b": "test2",
				"alpine": "docker-image://alpine@sha256:0123456789", "project": "https://github.com/myuser/project.git",
			}},
			want: &Options{NamedContexts: map[string]string{
				"a": filepath.Join(tmpwd, "test1"), "b": filepath.Join(tmpwd, "test2"),
				"alpine": "docker-image://alpine@sha256:0123456789", "project": "https://github.com/myuser/project.git",
			}},
		},
		{
			name: "cache-from",
			options: &Options{
				CacheFrom: []*pb.CacheOptionsEntry{
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
			want: &Options{
				CacheFrom: []*pb.CacheOptionsEntry{
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
			options: &Options{
				CacheTo: []*pb.CacheOptionsEntry{
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
			want: &Options{
				CacheTo: []*pb.CacheOptionsEntry{
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
			options: &Options{
				Exports: []*pb.ExportEntry{
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
			want: &Options{
				Exports: []*pb.ExportEntry{
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
			options: &Options{
				Secrets: []*pb.Secret{
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
			want: &Options{
				Secrets: []*pb.Secret{
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
			options: &Options{
				SSH: []*pb.SSH{
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
			want: &Options{
				SSH: []*pb.SSH{
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
			require.Equal(t, tt.want, got)
		})
	}
}
