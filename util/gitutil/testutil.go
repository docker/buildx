package gitutil

import (
	"os"
	"strings"
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

func GitAdd(c *Git, tb testing.TB, files ...string) {
	tb.Helper()
	args := append([]string{"add"}, files...)
	_, err := fakeGit(c, args...)
	require.NoError(tb, err)
}

func GitSetRemote(c *Git, tb testing.TB, name string, url string) {
	tb.Helper()
	_, err := fakeGit(c, "remote", "add", name, url)
	require.NoError(tb, err)
}

func GitSetMainUpstream(c *Git, tb testing.TB, remote, target string) {
	tb.Helper()
	_, err := fakeGit(c, "fetch", "--depth", "1", remote, target)
	require.NoError(tb, err)

	_, err = fakeGit(c, "branch", "--set-upstream-to", remote+"/"+target, "main")
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

func IsAmbiguousArgument(err error) bool {
	if err == nil {
		return false
	}
	errMsg := strings.ToLower(err.Error())
	return strings.Contains(errMsg, "use '--' to separate paths from revisions")
}
