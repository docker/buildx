package gitutil_test

import (
	"testing"

	"github.com/docker/buildx/util/gitutil"
	"github.com/docker/buildx/util/gitutil/gittestutil"
	"github.com/stretchr/testify/require"
)

func TestGit(t *testing.T) {
	c, err := gitutil.New()
	require.NoError(t, err)

	out, err := c.Run("status")
	require.NoError(t, err)
	require.NotEmpty(t, out)

	out, err = c.Run("not-exist")
	require.Error(t, err)
	require.Empty(t, out)
	require.Equal(t, "git: 'not-exist' is not a git command. See 'git --help'.", err.Error())
}

func TestGitFullCommit(t *testing.T) {
	gittestutil.Mktmp(t)
	c, err := gitutil.New()
	require.NoError(t, err)

	gittestutil.GitInit(c, t)
	gittestutil.GitCommit(c, t, "bar")

	out, err := c.FullCommit()
	require.NoError(t, err)
	require.Equal(t, 40, len(out))
}

func TestGitShortCommit(t *testing.T) {
	gittestutil.Mktmp(t)
	c, err := gitutil.New()
	require.NoError(t, err)

	gittestutil.GitInit(c, t)
	gittestutil.GitCommit(c, t, "bar")

	out, err := c.ShortCommit()
	require.NoError(t, err)
	require.Equal(t, 7, len(out))
}

func TestGitFullCommitErr(t *testing.T) {
	gittestutil.Mktmp(t)
	c, err := gitutil.New()
	require.NoError(t, err)

	gittestutil.GitInit(c, t)

	_, err = c.FullCommit()
	require.Error(t, err)
	require.True(t, gitutil.IsUnknownRevision(err))
	require.False(t, gittestutil.IsAmbiguousArgument(err))
}

func TestGitShortCommitErr(t *testing.T) {
	gittestutil.Mktmp(t)
	c, err := gitutil.New()
	require.NoError(t, err)

	gittestutil.GitInit(c, t)

	_, err = c.ShortCommit()
	require.Error(t, err)
	require.True(t, gitutil.IsUnknownRevision(err))
	require.False(t, gittestutil.IsAmbiguousArgument(err))
}

func TestGitTagsPointsAt(t *testing.T) {
	gittestutil.Mktmp(t)
	c, err := gitutil.New()
	require.NoError(t, err)

	gittestutil.GitInit(c, t)
	gittestutil.GitCommit(c, t, "bar")
	gittestutil.GitTag(c, t, "v0.8.0")
	gittestutil.GitCommit(c, t, "foo")
	gittestutil.GitTag(c, t, "v0.9.0")

	out, err := c.Run("tag", "--points-at", "HEAD", "--sort", "-version:creatordate")
	require.NoError(t, err)
	require.Equal(t, "v0.9.0", out)
}

func TestGitDescribeTags(t *testing.T) {
	gittestutil.Mktmp(t)
	c, err := gitutil.New()
	require.NoError(t, err)

	gittestutil.GitInit(c, t)
	gittestutil.GitCommit(c, t, "bar")
	gittestutil.GitTag(c, t, "v0.8.0")
	gittestutil.GitCommit(c, t, "foo")
	gittestutil.GitTag(c, t, "v0.9.0")

	out, err := c.Run("describe", "--tags", "--abbrev=0")
	require.NoError(t, err)
	require.Equal(t, "v0.9.0", out)
}

func TestGitRemoteURL(t *testing.T) {
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

			ru, err := c.RemoteURL()
			if tt.fail {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.expected, ru)
		})
	}
}
