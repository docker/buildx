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
			name: "contextpath",
			options: controllerapi.BuildOptions{
				Inputs: &controllerapi.Inputs{ContextPath: "test"},
			},
			want: controllerapi.BuildOptions{
				Inputs: &controllerapi.Inputs{ContextPath: filepath.Join(tmpwd, "test")},
			},
		},
		{
			name: "contextpath-cwd",
			options: controllerapi.BuildOptions{
				Inputs: &controllerapi.Inputs{ContextPath: "."},
			},
			want: controllerapi.BuildOptions{
				Inputs: &controllerapi.Inputs{ContextPath: tmpwd},
			},
		},
		{
			name: "contextpath-dash",
			options: controllerapi.BuildOptions{
				Inputs: &controllerapi.Inputs{ContextPath: "-"},
			},
			want: controllerapi.BuildOptions{
				Inputs: &controllerapi.Inputs{ContextPath: "-"},
			},
		},
		{
			name: "dockerfilename",
			options: controllerapi.BuildOptions{
				Inputs: &controllerapi.Inputs{DockerfileName: "test"},
			},
			want: controllerapi.BuildOptions{
				Inputs: &controllerapi.Inputs{DockerfileName: filepath.Join(tmpwd, "test")},
			},
		},
		{
			name: "dockerfilename-dash",
			options: controllerapi.BuildOptions{
				Inputs: &controllerapi.Inputs{DockerfileName: "-"},
			},
			want: controllerapi.BuildOptions{
				Inputs: &controllerapi.Inputs{DockerfileName: "-"},
			},
		},
		{
			name: "contexts",
			options: controllerapi.BuildOptions{
				Inputs: &controllerapi.Inputs{NamedContexts: map[string]*controllerapi.NamedContext{
					"a":       {Path: "test1"},
					"b":       {Path: "test2"},
					"alpine":  {Path: "docker-image://alpine@sha256:0123456789"},
					"project": {Path: "https://github.com/myuser/project.git"},
				},
				}},
			want: controllerapi.BuildOptions{
				Inputs: &controllerapi.Inputs{NamedContexts: map[string]*controllerapi.NamedContext{
					"a":       {Path: filepath.Join(tmpwd, "test1")},
					"b":       {Path: filepath.Join(tmpwd, "test2")},
					"alpine":  {Path: "docker-image://alpine@sha256:0123456789"},
					"project": {Path: "https://github.com/myuser/project.git"},
				},
				}},
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
		{
			name: "metadatafile",
			options: controllerapi.BuildOptions{
				Opts: &controllerapi.CommonOptions{
					MetadataFile: "test1",
				},
			},
			want: controllerapi.BuildOptions{
				Opts: &controllerapi.CommonOptions{
					MetadataFile: filepath.Join(tmpwd, "test1"),
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
