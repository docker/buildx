package commands

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	controllerapi "github.com/docker/buildx/controller/pb"
	"github.com/stretchr/testify/require"
)

func TestResolvePaths(t *testing.T) {
	tmpwd, err := os.MkdirTemp("", "testresolvepaths")
	require.NoError(t, err)
	defer os.Remove(tmpwd)
	require.NoError(t, os.Chdir(tmpwd))
	tests := []struct {
		name    string
		options controllerapi.BuildOptions
		want    controllerapi.BuildOptions
	}{
		{
			name:    "contextpath",
			options: controllerapi.BuildOptions{ContextPath: "test"},
			want:    controllerapi.BuildOptions{ContextPath: filepath.Join(tmpwd, "test")},
		},
		{
			name:    "contextpath-cwd",
			options: controllerapi.BuildOptions{ContextPath: "."},
			want:    controllerapi.BuildOptions{ContextPath: tmpwd},
		},
		{
			name:    "contextpath-dash",
			options: controllerapi.BuildOptions{ContextPath: "-"},
			want:    controllerapi.BuildOptions{ContextPath: "-"},
		},
		{
			name:    "contextpath-ssh",
			options: controllerapi.BuildOptions{ContextPath: "git@github.com:docker/buildx.git"},
			want:    controllerapi.BuildOptions{ContextPath: "git@github.com:docker/buildx.git"},
		},
		{
			name:    "dockerfilename",
			options: controllerapi.BuildOptions{DockerfileName: "test", ContextPath: "."},
			want:    controllerapi.BuildOptions{DockerfileName: filepath.Join(tmpwd, "test"), ContextPath: tmpwd},
		},
		{
			name:    "dockerfilename-dash",
			options: controllerapi.BuildOptions{DockerfileName: "-", ContextPath: "."},
			want:    controllerapi.BuildOptions{DockerfileName: "-", ContextPath: tmpwd},
		},
		{
			name:    "dockerfilename-remote",
			options: controllerapi.BuildOptions{DockerfileName: "test", ContextPath: "git@github.com:docker/buildx.git"},
			want:    controllerapi.BuildOptions{DockerfileName: "test", ContextPath: "git@github.com:docker/buildx.git"},
		},
		{
			name: "contexts",
			options: controllerapi.BuildOptions{NamedContexts: map[string]string{"a": "test1", "b": "test2",
				"alpine": "docker-image://alpine@sha256:0123456789", "project": "https://github.com/myuser/project.git"}},
			want: controllerapi.BuildOptions{NamedContexts: map[string]string{"a": filepath.Join(tmpwd, "test1"), "b": filepath.Join(tmpwd, "test2"),
				"alpine": "docker-image://alpine@sha256:0123456789", "project": "https://github.com/myuser/project.git"}},
		},
		{
			name: "cache-from",
			options: controllerapi.BuildOptions{
				CacheFrom: []*controllerapi.CacheOptionsEntry{
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
			want: controllerapi.BuildOptions{
				CacheFrom: []*controllerapi.CacheOptionsEntry{
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
			options: controllerapi.BuildOptions{
				CacheTo: []*controllerapi.CacheOptionsEntry{
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
			want: controllerapi.BuildOptions{
				CacheTo: []*controllerapi.CacheOptionsEntry{
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
			options: controllerapi.BuildOptions{
				Exports: []*controllerapi.ExportEntry{
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
			want: controllerapi.BuildOptions{
				Exports: []*controllerapi.ExportEntry{
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
			options: controllerapi.BuildOptions{
				Secrets: []*controllerapi.Secret{
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
			want: controllerapi.BuildOptions{
				Secrets: []*controllerapi.Secret{
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
			options: controllerapi.BuildOptions{
				SSH: []*controllerapi.SSH{
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
			want: controllerapi.BuildOptions{
				SSH: []*controllerapi.SSH{
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
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolvePaths(&tt.options)
			require.NoError(t, err)
			if !reflect.DeepEqual(tt.want, *got) {
				t.Fatalf("expected %#v, got %#v", tt.want, *got)
			}
		})
	}
}
