package gitutil

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGit(t *testing.T) {
	c, err := New()
	require.NoError(t, err)

	out, err := c.run("status")
	require.NoError(t, err)
	require.NotEmpty(t, out)

	out, err = c.clean(c.run("not-exist"))
	require.Error(t, err)
	require.Empty(t, out)
	require.Equal(t, "git: 'not-exist' is not a git command. See 'git --help'.", err.Error())
}

func TestGitFullCommit(t *testing.T) {
	Mktmp(t)
	c, err := New()
	require.NoError(t, err)

	GitInit(c, t)
	GitCommit(c, t, "bar")

	out, err := c.FullCommit()
	require.NoError(t, err)
	require.Equal(t, 40, len(out))
}

func TestGitShortCommit(t *testing.T) {
	Mktmp(t)
	c, err := New()
	require.NoError(t, err)

	GitInit(c, t)
	GitCommit(c, t, "bar")

	out, err := c.ShortCommit()
	require.NoError(t, err)
	require.Equal(t, 7, len(out))
}

func TestGitFullCommitErr(t *testing.T) {
	Mktmp(t)
	c, err := New()
	require.NoError(t, err)

	GitInit(c, t)

	_, err = c.FullCommit()
	require.Error(t, err)
	require.True(t, IsUnknownRevision(err))
	require.False(t, IsAmbiguousArgument(err))
}

func TestGitShortCommitErr(t *testing.T) {
	Mktmp(t)
	c, err := New()
	require.NoError(t, err)

	GitInit(c, t)

	_, err = c.ShortCommit()
	require.Error(t, err)
	require.True(t, IsUnknownRevision(err))
	require.False(t, IsAmbiguousArgument(err))
}

func TestGitTagsPointsAt(t *testing.T) {
	Mktmp(t)
	c, err := New()
	require.NoError(t, err)

	GitInit(c, t)
	GitCommit(c, t, "bar")
	GitTag(c, t, "v0.8.0")
	GitCommit(c, t, "foo")
	GitTag(c, t, "v0.9.0")

	out, err := c.clean(c.run("tag", "--points-at", "HEAD", "--sort", "-version:creatordate"))
	require.NoError(t, err)
	require.Equal(t, "v0.9.0", out)
}

func TestGitDescribeTags(t *testing.T) {
	Mktmp(t)
	c, err := New()
	require.NoError(t, err)

	GitInit(c, t)
	GitCommit(c, t, "bar")
	GitTag(c, t, "v0.8.0")
	GitCommit(c, t, "foo")
	GitTag(c, t, "v0.9.0")

	out, err := c.clean(c.run("describe", "--tags", "--abbrev=0"))
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
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			Mktmp(t)
			c, err := New()
			require.NoError(t, err)

			GitInit(c, t)
			GitCommit(c, t, "initial commit")
			for _, r := range tt.remotes {
				GitSetRemote(c, t, r.name, r.url)
				if r.tracking != "" {
					GitSetMainUpstream(c, t, r.name, r.tracking)
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

func TestStripCredentials(t *testing.T) {
	cases := []struct {
		name string
		url  string
		want string
	}{
		{
			name: "non-blank Password",
			url:  "https://user:password@host.tld/this:that",
			want: "https://host.tld/this:that",
		},
		{
			name: "blank Password",
			url:  "https://user@host.tld/this:that",
			want: "https://host.tld/this:that",
		},
		{
			name: "blank Username",
			url:  "https://:password@host.tld/this:that",
			want: "https://host.tld/this:that",
		},
		{
			name: "blank Username, blank Password",
			url:  "https://host.tld/this:that",
			want: "https://host.tld/this:that",
		},
		{
			name: "invalid URL",
			url:  "1https://foo.com",
			want: "1https://foo.com",
		},
	}
	for _, tt := range cases {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			if g, w := stripCredentials(tt.url), tt.want; g != w {
				t.Fatalf("got: %q\nwant: %q", g, w)
			}
		})
	}
}
