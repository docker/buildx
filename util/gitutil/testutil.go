package gitutil

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func GitInit(tb testing.TB) {
	tb.Helper()
	out, err := fakeGit("init")
	require.NoError(tb, err)
	require.Contains(tb, out, "Initialized empty Git repository")
	require.NoError(tb, err)
	GitCheckoutBranch(tb, "main")
	_, _ = fakeGit("branch", "-D", "master")
}

func GitCommit(tb testing.TB, msg string) {
	tb.Helper()
	out, err := fakeGit("commit", "--allow-empty", "-m", msg)
	require.NoError(tb, err)
	require.Contains(tb, out, "main", msg)
}

func GitTag(tb testing.TB, tag string) {
	tb.Helper()
	out, err := fakeGit("tag", tag)
	require.NoError(tb, err)
	require.Empty(tb, out)
}

func GitCheckoutBranch(tb testing.TB, name string) {
	tb.Helper()
	out, err := fakeGit("checkout", "-b", name)
	require.NoError(tb, err)
	require.Empty(tb, out)
}

func GitAdd(tb testing.TB, file string) {
	tb.Helper()
	_, err := fakeGit("add", file)
	require.NoError(tb, err)
}

func GitSetRemote(tb testing.TB, url string) {
	tb.Helper()
	_, err := fakeGit("remote", "add", "origin", url)
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

func fakeGit(args ...string) (string, error) {
	allArgs := []string{
		"-c", "user.name=buildx",
		"-c", "user.email=buildx@docker.com",
		"-c", "commit.gpgSign=false",
		"-c", "tag.gpgSign=false",
		"-c", "log.showSignature=false",
	}
	allArgs = append(allArgs, args...)
	c := New()
	return c.clean(c.run(allArgs...))
}
