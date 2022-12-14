package build

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/docker/buildx/util/gitutil"
	specs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
)

func setupTest(tb testing.TB) {
	gitutil.Mktmp(tb)
	gitutil.GitInit(tb)
	df := []byte("FROM alpine:latest\n")
	assert.NoError(tb, os.WriteFile("Dockerfile", df, 0644))
	gitutil.GitAdd(tb, "Dockerfile")
	gitutil.GitCommit(tb, "initial commit")
}

func TestGetGitAttributesNoContext(t *testing.T) {
	setupTest(t)

	gitattrs := getGitAttributes(context.Background(), "", "Dockerfile")
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
			},
		},
		{
			name:         "gitlabels",
			envGitLabels: "true",
			envGitInfo:   "false",
			expected: []string{
				"label:" + DockerfileLabel,
				"label:" + specs.AnnotationRevision,
			},
		},
		{
			name:         "both",
			envGitLabels: "true",
			envGitInfo:   "",
			expected: []string{
				"label:" + DockerfileLabel,
				"label:" + specs.AnnotationRevision,
				"vcs:revision",
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
			gitattrs := getGitAttributes(context.Background(), ".", "Dockerfile")
			for _, e := range tt.expected {
				assert.Contains(t, gitattrs, e)
				assert.NotEmpty(t, gitattrs[e])
				if e == "label:"+DockerfileLabel {
					assert.Equal(t, "Dockerfile", gitattrs[e])
				}
			}
		})
	}
}

func TestGetGitAttributesWithRemote(t *testing.T) {
	setupTest(t)
	gitutil.GitSetRemote(t, "git@github.com:docker/buildx.git")

	t.Setenv("BUILDX_GIT_LABELS", "true")
	gitattrs := getGitAttributes(context.Background(), ".", "Dockerfile")
	assert.Equal(t, 5, len(gitattrs))
	assert.Contains(t, gitattrs, "label:"+DockerfileLabel)
	assert.Equal(t, "Dockerfile", gitattrs["label:"+DockerfileLabel])
	assert.Contains(t, gitattrs, "label:"+specs.AnnotationRevision)
	assert.NotEmpty(t, gitattrs["label:"+specs.AnnotationRevision])
	assert.Contains(t, gitattrs, "label:"+specs.AnnotationSource)
	assert.Equal(t, "git@github.com:docker/buildx.git", gitattrs["label:"+specs.AnnotationSource])
	assert.Contains(t, gitattrs, "vcs:revision")
	assert.NotEmpty(t, gitattrs["vcs:revision"])
	assert.Contains(t, gitattrs, "vcs:source")
	assert.Equal(t, "git@github.com:docker/buildx.git", gitattrs["vcs:source"])
}

func TestGetGitAttributesDirty(t *testing.T) {
	setupTest(t)

	// make a change to test dirty flag
	df := []byte("FROM alpine:edge\n")
	assert.NoError(t, os.Mkdir("dir", 0755))
	assert.NoError(t, os.WriteFile(filepath.Join("dir", "Dockerfile"), df, 0644))

	t.Setenv("BUILDX_GIT_LABELS", "true")
	gitattrs := getGitAttributes(context.Background(), ".", "Dockerfile")
	assert.Equal(t, 3, len(gitattrs))
	assert.Contains(t, gitattrs, "label:"+DockerfileLabel)
	assert.Equal(t, "Dockerfile", gitattrs["label:"+DockerfileLabel])
	assert.Contains(t, gitattrs, "label:"+specs.AnnotationRevision)
	assert.True(t, strings.HasSuffix(gitattrs["label:"+specs.AnnotationRevision], "-dirty"))
	assert.Contains(t, gitattrs, "vcs:revision")
	assert.True(t, strings.HasSuffix(gitattrs["vcs:revision"], "-dirty"))
}
