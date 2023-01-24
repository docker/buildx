package build

import (
	"context"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"github.com/docker/buildx/util/gitutil"
	specs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTest(tb testing.TB) {
	gitutil.Mktmp(tb)

	c, err := gitutil.New()
	require.NoError(tb, err)
	gitutil.GitInit(c, tb)

	df := []byte("FROM alpine:latest\n")
	assert.NoError(tb, os.WriteFile("Dockerfile", df, 0644))

	gitutil.GitAdd(c, tb, "Dockerfile")
	gitutil.GitCommit(c, tb, "initial commit")
	gitutil.GitSetRemote(c, tb, "origin", "git@github.com:docker/buildx.git")
}

func TestGetGitAttributesNotGitRepo(t *testing.T) {
	_, err := getGitAttributes(context.Background(), t.TempDir(), "Dockerfile")
	assert.NoError(t, err)
}

func TestGetGitAttributesBadGitRepo(t *testing.T) {
	tmp := t.TempDir()
	require.NoError(t, os.MkdirAll(path.Join(tmp, ".git"), 0755))

	_, err := getGitAttributes(context.Background(), tmp, "Dockerfile")
	assert.Error(t, err)
}

func TestGetGitAttributesNoContext(t *testing.T) {
	setupTest(t)

	gitattrs, err := getGitAttributes(context.Background(), "", "Dockerfile")
	assert.NoError(t, err)
	assert.Empty(t, gitattrs)
}

func TestGetGitAttributes(t *testing.T) {
	cases := []struct {
		name         string
		envGitLabels string
		envGitInfo   string
		expected     []string
	}{
		{
			name:         "default",
			envGitLabels: "",
			envGitInfo:   "",
			expected: []string{
				"vcs:revision",
				"vcs:source",
			},
		},
		{
			name:         "none",
			envGitLabels: "false",
			envGitInfo:   "false",
			expected:     []string{},
		},
		{
			name:         "gitinfo",
			envGitLabels: "false",
			envGitInfo:   "true",
			expected: []string{
				"vcs:revision",
				"vcs:source",
			},
		},
		{
			name:         "gitlabels",
			envGitLabels: "true",
			envGitInfo:   "false",
			expected: []string{
				"label:" + DockerfileLabel,
				"label:" + specs.AnnotationRevision,
				"label:" + specs.AnnotationSource,
			},
		},
		{
			name:         "both",
			envGitLabels: "true",
			envGitInfo:   "",
			expected: []string{
				"label:" + DockerfileLabel,
				"label:" + specs.AnnotationRevision,
				"label:" + specs.AnnotationSource,
				"vcs:revision",
				"vcs:source",
			},
		},
	}
	for _, tt := range cases {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			setupTest(t)
			if tt.envGitLabels != "" {
				t.Setenv("BUILDX_GIT_LABELS", tt.envGitLabels)
			}
			if tt.envGitInfo != "" {
				t.Setenv("BUILDX_GIT_INFO", tt.envGitInfo)
			}
			gitattrs, err := getGitAttributes(context.Background(), ".", "Dockerfile")
			require.NoError(t, err)
			for _, e := range tt.expected {
				assert.Contains(t, gitattrs, e)
				assert.NotEmpty(t, gitattrs[e])
				if e == "label:"+DockerfileLabel {
					assert.Equal(t, "Dockerfile", gitattrs[e])
				} else if e == "label:"+specs.AnnotationSource || e == "vcs:source" {
					assert.Equal(t, "git@github.com:docker/buildx.git", gitattrs[e])
				}
			}
		})
	}
}

func TestGetGitAttributesDirty(t *testing.T) {
	setupTest(t)

	// make a change to test dirty flag
	df := []byte("FROM alpine:edge\n")
	require.NoError(t, os.Mkdir("dir", 0755))
	require.NoError(t, os.WriteFile(filepath.Join("dir", "Dockerfile"), df, 0644))

	t.Setenv("BUILDX_GIT_LABELS", "true")
	gitattrs, _ := getGitAttributes(context.Background(), ".", "Dockerfile")
	assert.Equal(t, 5, len(gitattrs))

	assert.Contains(t, gitattrs, "label:"+DockerfileLabel)
	assert.Equal(t, "Dockerfile", gitattrs["label:"+DockerfileLabel])
	assert.Contains(t, gitattrs, "label:"+specs.AnnotationSource)
	assert.Equal(t, "git@github.com:docker/buildx.git", gitattrs["label:"+specs.AnnotationSource])
	assert.Contains(t, gitattrs, "label:"+specs.AnnotationRevision)
	assert.True(t, strings.HasSuffix(gitattrs["label:"+specs.AnnotationRevision], "-dirty"))

	assert.Contains(t, gitattrs, "vcs:source")
	assert.Equal(t, "git@github.com:docker/buildx.git", gitattrs["vcs:source"])
	assert.Contains(t, gitattrs, "vcs:revision")
	assert.True(t, strings.HasSuffix(gitattrs["vcs:revision"], "-dirty"))
}
