package cloud

import (
	"io"
	"testing"

	"github.com/containerd/platforms"
	"github.com/docker/buildx/driver"
	"github.com/moby/buildkit/client"
	"github.com/moby/buildkit/exporter/containerimage/exptypes"
	sessionexporter "github.com/moby/buildkit/session/exporter"
	ocispecs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveCloudPullExporters(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                 string
		exports              []client.ExportEntry
		buildInfoAttrs       string
		buildInfoAttrsSet    bool
		noDefaultOCIArtifact bool
		wantExports          []client.ExportEntry
		wantRequests         []*sessionexporter.ExporterRequest
		wantContext          string
	}{
		{
			name: "Implicit",
			wantRequests: []*sessionexporter.ExporterRequest{{
				Type: "image",
				Attrs: map[string]string{
					"attestation-inline": "false",
					"name":               "example.com/image:latest",
				},
			}},
		},
		{
			name: "Docker",
			exports: []client.ExportEntry{{
				Type:  "docker",
				Attrs: map[string]string{"name": "example.com/image:latest", "context": "default"},
			}},
			wantRequests: []*sessionexporter.ExporterRequest{{
				Type: "image",
				Attrs: map[string]string{
					"attestation-inline": "false",
					"context":            "default",
					"name":               "example.com/image:latest",
				},
			}},
			wantContext: "default",
		},
		{
			name: "Image",
			exports: []client.ExportEntry{{
				Type: "image",
			}},
			wantRequests: []*sessionexporter.ExporterRequest{{
				Type: "image",
				Attrs: map[string]string{
					"attestation-inline": "false",
					"name":               "example.com/image:latest",
				},
			}},
		},
		{
			name: "Push",
			exports: []client.ExportEntry{{
				Type:  "image",
				Attrs: map[string]string{"push": "true"},
			}},
			wantExports: []client.ExportEntry{{
				Type:  "image",
				Attrs: map[string]string{"push": "true"},
			}},
		},
		{
			name: "OCI",
			exports: []client.ExportEntry{{
				Type: "oci",
			}},
			wantExports: []client.ExportEntry{{
				Type: "oci",
			}},
		},
		{
			name: "CacheOnly",
			exports: []client.ExportEntry{{
				Type: "cacheonly",
			}},
			wantExports: []client.ExportEntry{{
				Type: "cacheonly",
			}},
		},
		{
			name: "Mixed",
			exports: []client.ExportEntry{
				{Type: "docker", Attrs: map[string]string{"compression": "zstd"}},
				{Type: "image", Attrs: map[string]string{"push": "true"}},
				{Type: "oci"},
			},
			wantExports: []client.ExportEntry{
				{Type: "image", Attrs: map[string]string{"push": "true"}},
				{Type: "oci"},
			},
			wantRequests: []*sessionexporter.ExporterRequest{{
				Type: "image",
				Attrs: map[string]string{
					"attestation-inline": "false",
					"compression":        "zstd",
					"name":               "example.com/image:latest",
				},
			}},
		},
		{
			name: "GenericAttrs",
			exports: []client.ExportEntry{{
				Type:  "docker",
				Attrs: map[string]string{"compression": "zstd"},
			}},
			buildInfoAttrs:       "context,source",
			buildInfoAttrsSet:    true,
			noDefaultOCIArtifact: true,
			wantRequests: []*sessionexporter.ExporterRequest{{
				Type: "image",
				Attrs: map[string]string{
					"attestation-inline":               "false",
					"buildinfo-attrs":                  "context,source",
					"compression":                      "zstd",
					"name":                             "example.com/image:latest",
					string(exptypes.OptKeyOCIArtifact): "false",
				},
			}},
		},
		{
			name: "ExplicitOCIArtifact",
			exports: []client.ExportEntry{{
				Type: "image",
				Attrs: map[string]string{
					string(exptypes.OptKeyOCIArtifact): "true",
				},
			}},
			noDefaultOCIArtifact: true,
			wantRequests: []*sessionexporter.ExporterRequest{{
				Type: "image",
				Attrs: map[string]string{
					"attestation-inline":               "false",
					"name":                             "example.com/image:latest",
					string(exptypes.OptKeyOCIArtifact): "true",
				},
			}},
		},
		{
			name: "MultipleCloudPullRequests",
			exports: []client.ExportEntry{
				{Type: "docker", Attrs: map[string]string{"compression": "zstd"}},
				{Type: "image", Attrs: map[string]string{"oci-mediatypes": "true"}},
			},
			wantExports: []client.ExportEntry{
				{Type: "docker", Attrs: map[string]string{"compression": "zstd"}},
				{Type: "image", Attrs: map[string]string{"oci-mediatypes": "true"}},
			},
		},
		{
			name: "MultipleMatchingContexts",
			exports: []client.ExportEntry{
				{Type: "docker", Attrs: map[string]string{"context": "staging"}},
				{Type: "docker", Attrs: map[string]string{"context": "staging"}},
			},
			wantRequests: []*sessionexporter.ExporterRequest{{
				Type: "image",
				Attrs: map[string]string{
					"attestation-inline": "false",
					"context":            "staging",
					"name":               "example.com/image:latest",
				},
			}},
			wantContext: "staging",
		},
		{
			name: "MixedCurrentAndNamedContext",
			exports: []client.ExportEntry{
				{Type: "docker"},
				{Type: "docker", Attrs: map[string]string{"context": "staging"}},
			},
			wantExports: []client.ExportEntry{
				{Type: "docker"},
				{Type: "docker", Attrs: map[string]string{"context": "staging"}},
			},
		},
		{
			name: "ConflictingContexts",
			exports: []client.ExportEntry{
				{Type: "docker", Attrs: map[string]string{"context": "staging"}},
				{Type: "docker", Attrs: map[string]string{"context": "prod"}},
			},
			wantExports: []client.ExportEntry{
				{Type: "docker", Attrs: map[string]string{"context": "staging"}},
				{Type: "docker", Attrs: map[string]string{"context": "prod"}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			exports, requests, dockerContext := resolveCloudPullExporters(tt.exports, []string{"example.com/image:latest"}, tt.buildInfoAttrs, tt.buildInfoAttrsSet, tt.noDefaultOCIArtifact)
			assert.Equal(t, tt.wantExports, exports)
			assert.Equal(t, tt.wantRequests, requests)
			assert.Equal(t, tt.wantContext, dockerContext)
		})
	}
}

func TestResolveCloudPullExportersClonesAttrs(t *testing.T) {
	t.Parallel()

	exports := []client.ExportEntry{{
		Type:  "docker",
		Attrs: map[string]string{"compression": "zstd"},
	}}
	_, requests, _ := resolveCloudPullExporters(exports, []string{"example.com/image:latest"}, "", false, false)
	require.Len(t, requests, 1)
	requests[0].Attrs["new"] = "value"
	assert.NotContains(t, exports[0].Attrs, "attestation-inline")
	assert.NotContains(t, exports[0].Attrs, "new")
}

func TestResolveCloudPullExportersKeepsExplicitDockerOutput(t *testing.T) {
	t.Parallel()

	output := func(map[string]string) (io.WriteCloser, error) {
		return nil, nil
	}
	exports := []client.ExportEntry{
		{Type: "docker", Output: output},
		{Type: "docker", OutputDir: "out"},
	}

	gotExports, requests, dockerContext := resolveCloudPullExporters(exports, []string{"example.com/image:latest"}, "", false, false)

	require.Empty(t, requests)
	assert.Empty(t, dockerContext)
	assert.Len(t, gotExports, 2)
	assert.NotNil(t, gotExports[0].Output)
	assert.Equal(t, "out", gotExports[1].OutputDir)
}

func TestPrepareBuildWithoutTags(t *testing.T) {
	t.Parallel()

	exports := []client.ExportEntry{{Type: "docker"}}
	solveOpt := client.SolveOpt{
		Exports: exports,
	}
	opt := driver.PrepareBuildOptions{
		SolveOpt: &solveOpt,
	}
	require.NoError(t, (&Driver{}).PrepareBuild(t.Context(), &opt))
	assert.Equal(t, exports, solveOpt.Exports)
	assert.Empty(t, solveOpt.Session)
	assert.False(t, solveOpt.EnableSessionExporter)
}

func TestPrepareBuildNoOutput(t *testing.T) {
	t.Parallel()

	solveOpt := client.SolveOpt{}
	opt := driver.PrepareBuildOptions{
		SolveOpt: &solveOpt,
		Tags:     []string{"example.com/image:latest"},
		NoOutput: true,
	}

	require.NoError(t, (&Driver{}).PrepareBuild(t.Context(), &opt))
	assert.Empty(t, solveOpt.Exports)
	assert.Empty(t, solveOpt.Session)
	assert.False(t, solveOpt.EnableSessionExporter)
}

func TestPrepareBuildUnsupportedCloudPullFallsBack(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		multiDriver bool
		platforms   []ocispecs.Platform
	}{
		{
			name:        "MultiDriver",
			multiDriver: true,
		},
		{
			name: "MultiPlatform",
			platforms: []ocispecs.Platform{
				{OS: "linux", Architecture: "amd64"},
				{OS: "linux", Architecture: "arm64"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			exports := []client.ExportEntry{{Type: "docker"}}
			solveOpt := client.SolveOpt{
				Exports: exports,
			}
			opt := driver.PrepareBuildOptions{
				SolveOpt:    &solveOpt,
				Tags:        []string{"example.com/image:latest"},
				Platforms:   tt.platforms,
				MultiDriver: tt.multiDriver,
			}

			require.NoError(t, (&Driver{}).PrepareBuild(t.Context(), &opt))
			assert.Equal(t, exports, solveOpt.Exports)
			assert.Empty(t, solveOpt.Session)
			assert.False(t, solveOpt.EnableSessionExporter)
		})
	}
}

func TestCloudPullSessionExporter(t *testing.T) {
	t.Parallel()

	requests := []*sessionexporter.ExporterRequest{{
		Type:  "image",
		Attrs: map[string]string{"name": "example.com/image:latest"},
	}}
	provider := (&Driver{}).newCloudPullSessionExporter(driver.PrepareBuildOptions{}, requests, "")

	response, err := provider.FindExporters(t.Context(), &sessionexporter.FindExportersRequest{})
	require.NoError(t, err)
	assert.Equal(t, requests, response.Exporters)
}

func TestCloudPullPlatform(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		nodePlatforms []ocispecs.Platform
		optPlatforms  []ocispecs.Platform
		want          string
	}{
		{
			name: "RequestedPlatform",
			nodePlatforms: []ocispecs.Platform{
				platforms.MustParse("linux/arm64"),
			},
			optPlatforms: []ocispecs.Platform{
				platforms.MustParse("linux/amd64"),
			},
			want: "linux/amd64",
		},
		{
			name: "SingleNodePlatform",
			nodePlatforms: []ocispecs.Platform{
				platforms.MustParse("linux/arm64"),
			},
			want: "linux/arm64",
		},
		{
			name: "MultipleNodePlatforms",
			nodePlatforms: []ocispecs.Platform{
				platforms.MustParse("linux/amd64"),
				platforms.MustParse("linux/arm64"),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			d := &Driver{
				InitConfig: driver.InitConfig{
					Platforms: tt.nodePlatforms,
				},
			}
			got := d.cloudPullPlatform(driver.PrepareBuildOptions{
				Platforms: tt.optPlatforms,
			})
			assert.Equal(t, tt.want, got)
		})
	}
}
