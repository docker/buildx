package gitutil_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/docker/buildx/util/gitutil"
	"github.com/docker/buildx/util/gitutil/gittestutil"
	"github.com/stretchr/testify/require"
)

func TestGit(t *testing.T) {
	ctx := context.TODO()
	gittestutil.Mktmp(t)
	c, err := gitutil.New()
	require.NoError(t, err)

	gittestutil.GitInit(c, t)
	gittestutil.GitCommit(c, t, "bar")

	out, err := c.Run(ctx, "status")
	require.NoError(t, err)
	require.NotEmpty(t, out)

	out, err = c.Run(ctx, "not-exist")
	require.Error(t, err)
	require.Empty(t, out)
	require.Contains(t, err.Error(), "git: 'not-exist' is not a git command. See 'git --help'.")
}

func TestGitFullCommit(t *testing.T) {
	ctx := context.TODO()
	gittestutil.Mktmp(t)
	c, err := gitutil.New()
	require.NoError(t, err)

	gittestutil.GitInit(c, t)
	gittestutil.GitCommit(c, t, "bar")

	out, err := c.FullCommit(ctx)
	require.NoError(t, err)
	require.Equal(t, 40, len(out))
}

func TestGitShortCommit(t *testing.T) {
	ctx := context.TODO()
	gittestutil.Mktmp(t)
	c, err := gitutil.New()
	require.NoError(t, err)

	gittestutil.GitInit(c, t)
	gittestutil.GitCommit(c, t, "bar")

	out, err := c.ShortCommit(ctx)
	require.NoError(t, err)
	require.Equal(t, 7, len(out))
}

func TestGitFullCommitErr(t *testing.T) {
	ctx := context.TODO()
	gittestutil.Mktmp(t)
	c, err := gitutil.New()
	require.NoError(t, err)

	gittestutil.GitInit(c, t)

	_, err = c.FullCommit(ctx)
	require.Error(t, err)
	require.True(t, gitutil.IsUnknownRevision(err))
	require.False(t, gittestutil.IsAmbiguousArgument(err))
}

func TestGitShortCommitErr(t *testing.T) {
	ctx := context.TODO()
	gittestutil.Mktmp(t)
	c, err := gitutil.New()
	require.NoError(t, err)

	gittestutil.GitInit(c, t)

	_, err = c.ShortCommit(ctx)
	require.Error(t, err)
	require.True(t, gitutil.IsUnknownRevision(err))
	require.False(t, gittestutil.IsAmbiguousArgument(err))
}

func TestGitCommitWithHEADFile(t *testing.T) {
	ctx := context.TODO()
	gittestutil.Mktmp(t)
	c, err := gitutil.New()
	require.NoError(t, err)
	gittestutil.GitInit(c, t)
	require.NoError(t, os.WriteFile("HEAD", []byte("not a revision"), 0600))

	full, err := c.FullCommit(ctx)
	require.Error(t, err)
	require.True(t, gitutil.IsUnknownRevision(err))
	require.Empty(t, full)
	short, err := c.ShortCommit(ctx)
	require.Error(t, err)
	require.True(t, gitutil.IsUnknownRevision(err))
	require.Empty(t, short)
}

func TestGitCommitWithoutParentBlob(t *testing.T) {
	ctx := context.TODO()
	gittestutil.Mktmp(t)
	c, err := gitutil.New()
	require.NoError(t, err)
	gittestutil.GitInit(c, t)
	require.NoError(t, os.WriteFile("file", []byte("before"), 0600))
	gittestutil.GitAdd(c, t, "file")
	gittestutil.GitCommit(c, t, "before")
	blob, err := c.Run(ctx, "rev-parse", "HEAD:file")
	require.NoError(t, err)
	_, err = c.Run(ctx, "config", "diff.renames", "true")
	require.NoError(t, err)
	require.NoError(t, os.Remove("file"))
	require.NoError(t, os.WriteFile("other", []byte("something else entirely"), 0600))
	gittestutil.GitAdd(c, t, "file", "other")
	gittestutil.GitCommit(c, t, "after")
	want, err := c.FullCommit(ctx)
	require.NoError(t, err)
	wantShort, err := c.ShortCommit(ctx)
	require.NoError(t, err)

	// git show reads the parent blob for rename detection even with --quiet.
	// Resolving HEAD should not need that blob.
	hash := strings.TrimSpace(string(blob))
	require.NoError(t, os.Remove(filepath.Join(".git", "objects", hash[:2], hash[2:])))
	full, err := c.FullCommit(ctx)
	require.NoError(t, err)
	require.Equal(t, want, full)
	short, err := c.ShortCommit(ctx)
	require.NoError(t, err)
	require.Equal(t, wantShort, short)
}

func TestGitTagsPointsAt(t *testing.T) {
	ctx := context.TODO()
	gittestutil.Mktmp(t)
	c, err := gitutil.New()
	require.NoError(t, err)

	gittestutil.GitInit(c, t)
	gittestutil.GitCommit(c, t, "bar")
	gittestutil.GitTag(c, t, "v0.8.0")
	gittestutil.GitCommit(c, t, "foo")
	gittestutil.GitTag(c, t, "v0.9.0")

	out, err := c.Run(ctx, "tag", "--points-at", "HEAD", "--sort", "-version:creatordate")
	require.NoError(t, err)
	require.Equal(t, "v0.9.0", strings.TrimSpace(string(out)))
}

func TestGitDescribeTags(t *testing.T) {
	ctx := context.TODO()
	gittestutil.Mktmp(t)
	c, err := gitutil.New()
	require.NoError(t, err)

	gittestutil.GitInit(c, t)
	gittestutil.GitCommit(c, t, "bar")
	gittestutil.GitTag(c, t, "v0.8.0")
	gittestutil.GitCommit(c, t, "foo")
	gittestutil.GitTag(c, t, "v0.9.0")

	out, err := c.Run(ctx, "describe", "--tags", "--abbrev=0")
	require.NoError(t, err)
	require.Equal(t, "v0.9.0", strings.TrimSpace(string(out)))
}

func TestGitRemoteURL(t *testing.T) {
	ctx := context.TODO()
	type remote struct {
		name     string
		url      string
		tracking string
	}

	cases := []struct {
		name     string
		remotes  []remote
		expected string
		fail     bool
	}{
		{
			name:    "no remotes",
			remotes: []remote{},
			fail:    true,
		},
		{
			name: "origin",
			remotes: []remote{
				{
					name: "origin",
					url:  "git@github.com:crazy-max/buildx.git",
				},
			},
			expected: "git@github.com:crazy-max/buildx.git",
		},
		{
			name: "upstream",
			remotes: []remote{
				{
					name: "upstream",
					url:  "git@github.com:docker/buildx.git",
				},
			},
			expected: "git@github.com:docker/buildx.git",
		},
		{
			name: "origin and upstream",
			remotes: []remote{
				{
					name: "upstream",
					url:  "git@github.com:docker/buildx.git",
				},
				{
					name: "origin",
					url:  "git@github.com:crazy-max/buildx.git",
				},
			},
			expected: "git@github.com:crazy-max/buildx.git",
		},
		{
			name: "not found",
			remotes: []remote{
				{
					name: "foo",
					url:  "git@github.com:docker/buildx.git",
				},
			},
			fail: true,
		},
		{
			name: "single tracking branch",
			remotes: []remote{
				{
					name:     "foo",
					url:      "https://github.com/tonistiigi/buildx.git",
					tracking: "master",
				},
			},
			expected: "https://github.com/tonistiigi/buildx.git",
		},
		{
			name: "fork tracking branch",
			remotes: []remote{
				{
					name: "origin",
					url:  "git@github.com:crazy-max/buildx.git",
				},
				{
					name:     "foobranch",
					url:      "https://github.com/tonistiigi/buildx.git",
					tracking: "master",
				},
				{
					name: "crazymax",
					url:  "git@github.com:crazy-max/buildx.git",
				},
			},
			expected: "https://github.com/tonistiigi/buildx.git",
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			gittestutil.Mktmp(t)
			c, err := gitutil.New()
			require.NoError(t, err)

			gittestutil.GitInit(c, t)
			gittestutil.GitCommit(c, t, "initial commit")
			for _, r := range tt.remotes {
				gittestutil.GitSetRemote(c, t, r.name, r.url)
				if r.tracking != "" {
					gittestutil.GitSetMainUpstream(c, t, r.name, r.tracking)
				}
			}

			ru, err := c.RemoteURL(ctx)
			if tt.fail {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.expected, ru)
		})
	}
}
