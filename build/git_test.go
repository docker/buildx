package build

import (
	"context"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"github.com/docker/buildx/util/gitutil"
	"github.com/docker/buildx/util/gitutil/gittestutil"
	"github.com/moby/buildkit/client"
	ocispecs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTest(tb testing.TB) {
	gittestutil.Mktmp(tb)

	c, err := gitutil.New()
	require.NoError(tb, err)
	gittestutil.GitInit(c, tb)

	df := []byte("FROM alpine:latest\n")
	require.NoError(tb, os.WriteFile("Dockerfile", df, 0644))

	gittestutil.GitAdd(c, tb, "Dockerfile")
	gittestutil.GitCommit(c, tb, "initial commit")
	gittestutil.GitSetRemote(c, tb, "origin", "git@github.com:docker/buildx.git")
}

func TestGetGitAttributesNotGitRepo(t *testing.T) {
	_, err := getGitAttributes(context.Background(), t.TempDir(), "Dockerfile")
	require.NoError(t, err)
}

func TestGetGitAttributesBadGitRepo(t *testing.T) {
	tmp := t.TempDir()
	require.NoError(t, os.MkdirAll(path.Join(tmp, ".git"), 0755))

	_, err := getGitAttributes(context.Background(), tmp, "Dockerfile")
	assert.Error(t, err)
}

func TestGetGitAttributesNoContext(t *testing.T) {
	setupTest(t)

	addGitAttrs, err := getGitAttributes(context.Background(), "", "Dockerfile")
	require.NoError(t, err)
	var so client.SolveOpt
	addGitAttrs(&so)
	assert.Empty(t, so.FrontendAttrs)
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
				"label:" + ocispecs.AnnotationRevision,
				"label:" + ocispecs.AnnotationSource,
			},
		},
		{
			name:         "both",
			envGitLabels: "true",
			envGitInfo:   "",
			expected: []string{
				"label:" + DockerfileLabel,
				"label:" + ocispecs.AnnotationRevision,
				"label:" + ocispecs.AnnotationSource,
				"vcs:revision",
				"vcs:source",
			},
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			setupTest(t)
			if tt.envGitLabels != "" {
				t.Setenv("BUILDX_GIT_LABELS", tt.envGitLabels)
			}
			if tt.envGitInfo != "" {
				t.Setenv("BUILDX_GIT_INFO", tt.envGitInfo)
			}
			addGitAttrs, err := getGitAttributes(context.Background(), ".", "Dockerfile")
			require.NoError(t, err)
			var so client.SolveOpt
			addGitAttrs(&so)
			for _, e := range tt.expected {
				assert.Contains(t, so.FrontendAttrs, e)
				assert.NotEmpty(t, so.FrontendAttrs[e])
				switch e {
				case "label:" + DockerfileLabel:
					assert.Equal(t, "Dockerfile", so.FrontendAttrs[e])
				case "label:" + ocispecs.AnnotationSource, "vcs:source":
					assert.Equal(t, "git@github.com:docker/buildx.git", so.FrontendAttrs[e])
				}
			}
		})
	}
}

func TestGetGitAttributesDirty(t *testing.T) {
	setupTest(t)
	t.Setenv("BUILDX_GIT_CHECK_DIRTY", "true")

	// make a change to test dirty flag
	df := []byte("FROM alpine:edge\n")
	require.NoError(t, os.Mkdir("dir", 0755))
	require.NoError(t, os.WriteFile(filepath.Join("dir", "Dockerfile"), df, 0644))

	t.Setenv("BUILDX_GIT_LABELS", "true")
	addGitAttrs, err := getGitAttributes(context.Background(), ".", "Dockerfile")
	require.NoError(t, err)

	var so client.SolveOpt
	addGitAttrs(&so)

	assert.Equal(t, 5, len(so.FrontendAttrs))

	assert.Contains(t, so.FrontendAttrs, "label:"+DockerfileLabel)
	assert.Equal(t, "Dockerfile", so.FrontendAttrs["label:"+DockerfileLabel])
	assert.Contains(t, so.FrontendAttrs, "label:"+ocispecs.AnnotationSource)
	assert.Equal(t, "git@github.com:docker/buildx.git", so.FrontendAttrs["label:"+ocispecs.AnnotationSource])
	assert.Contains(t, so.FrontendAttrs, "label:"+ocispecs.AnnotationRevision)
	assert.True(t, strings.HasSuffix(so.FrontendAttrs["label:"+ocispecs.AnnotationRevision], "-dirty"))

	assert.Contains(t, so.FrontendAttrs, "vcs:source")
	assert.Equal(t, "git@github.com:docker/buildx.git", so.FrontendAttrs["vcs:source"])
	assert.Contains(t, so.FrontendAttrs, "vcs:revision")
	assert.True(t, strings.HasSuffix(so.FrontendAttrs["vcs:revision"], "-dirty"))
}

func TestLocalDirs(t *testing.T) {
	setupTest(t)

	so := &client.SolveOpt{
		FrontendAttrs: map[string]string{},
	}

	addGitAttrs, err := getGitAttributes(context.Background(), ".", "Dockerfile")
	require.NoError(t, err)

	require.NoError(t, setLocalMount("context", ".", so))
	require.NoError(t, setLocalMount("dockerfile", ".", so))

	addGitAttrs(so)

	require.Contains(t, so.FrontendAttrs, "vcs:localdir:context")
	assert.Equal(t, ".", so.FrontendAttrs["vcs:localdir:context"])

	require.Contains(t, so.FrontendAttrs, "vcs:localdir:dockerfile")
	assert.Equal(t, ".", so.FrontendAttrs["vcs:localdir:dockerfile"])
}

func TestLocalDirsSub(t *testing.T) {
	gittestutil.Mktmp(t)

	c, err := gitutil.New()
	require.NoError(t, err)
	gittestutil.GitInit(c, t)

	df := []byte("FROM alpine:latest\n")
	require.NoError(t, os.MkdirAll("app", 0755))
	require.NoError(t, os.WriteFile("app/Dockerfile", df, 0644))

	gittestutil.GitAdd(c, t, "app/Dockerfile")
	gittestutil.GitCommit(c, t, "initial commit")
	gittestutil.GitSetRemote(c, t, "origin", "git@github.com:docker/buildx.git")

	so := &client.SolveOpt{
		FrontendAttrs: map[string]string{},
	}
	require.NoError(t, setLocalMount("context", ".", so))
	require.NoError(t, setLocalMount("dockerfile", "app", so))

	addGitAttrs, err := getGitAttributes(context.Background(), ".", "app/Dockerfile")
	require.NoError(t, err)

	addGitAttrs(so)

	require.Contains(t, so.FrontendAttrs, "vcs:localdir:context")
	assert.Equal(t, ".", so.FrontendAttrs["vcs:localdir:context"])

	require.Contains(t, so.FrontendAttrs, "vcs:localdir:dockerfile")
	assert.Equal(t, "app", so.FrontendAttrs["vcs:localdir:dockerfile"])
}
