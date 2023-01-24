package gitutil

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func GitInit(c *Git, tb testing.TB) {
	tb.Helper()
	out, err := fakeGit(c, "init")
	require.NoError(tb, err)
	require.Contains(tb, out, "Initialized empty Git repository")
	require.NoError(tb, err)
	GitCheckoutBranch(c, tb, "main")
	_, _ = fakeGit(c, "branch", "-D", "master")
}

func GitCommit(c *Git, tb testing.TB, msg string) {
	tb.Helper()
	out, err := fakeGit(c, "commit", "--allow-empty", "-m", msg)
	require.NoError(tb, err)
	require.Contains(tb, out, "main", msg)
}

func GitTag(c *Git, tb testing.TB, tag string) {
	tb.Helper()
	out, err := fakeGit(c, "tag", tag)
	require.NoError(tb, err)
	require.Empty(tb, out)
}

func GitCheckoutBranch(c *Git, tb testing.TB, name string) {
	tb.Helper()
	out, err := fakeGit(c, "checkout", "-b", name)
	require.NoError(tb, err)
	require.Empty(tb, out)
}

func GitAdd(c *Git, tb testing.TB, file string) {
	tb.Helper()
	_, err := fakeGit(c, "add", file)
	require.NoError(tb, err)
}

func GitSetRemote(c *Git, tb testing.TB, name string, url string) {
	tb.Helper()
	_, err := fakeGit(c, "remote", "add", name, url)
	require.NoError(tb, err)
}

func Mktmp(tb testing.TB) string {
	tb.Helper()
	folder := tb.TempDir()
	current, err := os.Getwd()
	require.NoError(tb, err)
	require.NoError(tb, os.Chdir(folder))
	tb.Cleanup(func() {
		require.NoError(tb, os.Chdir(current))
	})
	return folder
}

func fakeGit(c *Git, args ...string) (string, error) {
	allArgs := []string{
		"-c", "user.name=buildx",
		"-c", "user.email=buildx@docker.com",
		"-c", "commit.gpgSign=false",
		"-c", "tag.gpgSign=false",
		"-c", "log.showSignature=false",
	}
	allArgs = append(allArgs, args...)
	return c.clean(c.run(allArgs...))
}
