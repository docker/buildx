package tests

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/containerd/continuity/fs/fstest"
	"github.com/docker/buildx/util/gitutil"
	"github.com/moby/buildkit/util/testutil/integration"
	"github.com/stretchr/testify/require"
)

func bakeCmd(sb integration.Sandbox, opts ...cmdOpt) (string, error) {
	opts = append([]cmdOpt{withArgs("bake", "--progress=quiet")}, opts...)
	cmd := buildxCmd(sb, opts...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

var bakeTests = []func(t *testing.T, sb integration.Sandbox){
	testBakeLocal,
	testBakeRemote,
	testBakeRemoteCmdContext,
	testBakeRemoteCmdContextOverride,
	testBakeRemoteContextSubdir,
	testBakeRemoteCmdContextEscapeRoot,
	testBakeRemoteCmdContextEscapeRelative,
	testBakeRemoteDockerfileCwd,
}

func testBakeLocal(t *testing.T, sb integration.Sandbox) {
	dockerfile := []byte(`
FROM scratch
COPY foo /foo
	`)
	bakefile := []byte(`
target "default" {
}
`)
	dir := tmpdir(
		t,
		fstest.CreateFile("docker-bake.hcl", bakefile, 0600),
		fstest.CreateFile("Dockerfile", dockerfile, 0600),
		fstest.CreateFile("foo", []byte("foo"), 0600),
	)
	dirDest := t.TempDir()

	out, err := bakeCmd(sb, withDir(dir), withArgs("--set", "*.output=type=local,dest="+dirDest))
	require.NoError(t, err, out)

	require.FileExists(t, filepath.Join(dirDest, "foo"))
}

func testBakeRemote(t *testing.T, sb integration.Sandbox) {
	bakefile := []byte(`
target "default" {
	dockerfile-inline = <<EOT
FROM scratch
COPY foo /foo
EOT
}
`)
	dir := tmpdir(
		t,
		fstest.CreateFile("docker-bake.hcl", bakefile, 0600),
		fstest.CreateFile("foo", []byte("foo"), 0600),
	)
	dirDest := t.TempDir()

	git, err := gitutil.New(gitutil.WithWorkingDir(dir))
	require.NoError(t, err)

	gitutil.GitInit(git, t)
	gitutil.GitAdd(git, t, "docker-bake.hcl", "foo")
	gitutil.GitCommit(git, t, "initial commit")
	addr := gitutil.GitServeHTTP(git, t)

	out, err := bakeCmd(sb, withDir(dir), withArgs(addr, "--set", "*.output=type=local,dest="+dirDest))
	require.NoError(t, err, out)

	require.FileExists(t, filepath.Join(dirDest, "foo"))
}

func testBakeRemoteCmdContext(t *testing.T, sb integration.Sandbox) {
	bakefile := []byte(`
target "default" {
	context = BAKE_CMD_CONTEXT
	dockerfile-inline = <<EOT
FROM scratch
COPY foo /foo
EOT
}
`)
	dirSpec := tmpdir(
		t,
		fstest.CreateFile("docker-bake.hcl", bakefile, 0600),
	)
	dirSrc := tmpdir(
		t,
		fstest.CreateFile("foo", []byte("foo"), 0600),
	)
	dirDest := t.TempDir()

	git, err := gitutil.New(gitutil.WithWorkingDir(dirSpec))
	require.NoError(t, err)

	gitutil.GitInit(git, t)
	gitutil.GitAdd(git, t, "docker-bake.hcl")
	gitutil.GitCommit(git, t, "initial commit")
	addr := gitutil.GitServeHTTP(git, t)

	out, err := bakeCmd(sb, withDir(dirSrc), withArgs(addr, "--set", "*.output=type=local,dest="+dirDest))
	require.NoError(t, err, out)

	require.FileExists(t, filepath.Join(dirDest, "foo"))
}

func testBakeRemoteCmdContextOverride(t *testing.T, sb integration.Sandbox) {
	bakefile := []byte(`
target "default" {
	context = BAKE_CMD_CONTEXT
	dockerfile-inline = <<EOT
FROM scratch
COPY foo /foo
EOT
}
`)
	dirSpec := tmpdir(
		t,
		fstest.CreateFile("docker-bake.hcl", bakefile, 0600),
	)
	dirSrc := tmpdir(
		t,
		fstest.CreateFile("foo", []byte("foo"), 0600),
	)
	dirDest := t.TempDir()

	gitSpec, err := gitutil.New(gitutil.WithWorkingDir(dirSpec))
	require.NoError(t, err)
	gitutil.GitInit(gitSpec, t)
	gitutil.GitAdd(gitSpec, t, "docker-bake.hcl")
	gitutil.GitCommit(gitSpec, t, "initial commit")
	addrSpec := gitutil.GitServeHTTP(gitSpec, t)

	gitSrc, err := gitutil.New(gitutil.WithWorkingDir(dirSrc))
	require.NoError(t, err)
	gitutil.GitInit(gitSrc, t)
	gitutil.GitAdd(gitSrc, t, "foo")
	gitutil.GitCommit(gitSrc, t, "initial commit")
	addrSrc := gitutil.GitServeHTTP(gitSrc, t)

	out, err := bakeCmd(sb, withDir("/tmp"), withArgs(addrSpec, addrSrc, "--set", "*.output=type=local,dest="+dirDest))
	require.NoError(t, err, out)

	require.FileExists(t, filepath.Join(dirDest, "foo"))
}

// https://github.com/docker/buildx/issues/1738
func testBakeRemoteContextSubdir(t *testing.T, sb integration.Sandbox) {
	bakefile := []byte(`
target default {
	context = "./bar"
}
`)
	dockerfile := []byte(`
FROM scratch
COPY super-cool.txt /
`)

	dir := tmpdir(
		t,
		fstest.CreateFile("docker-bake.hcl", bakefile, 0600),
		fstest.CreateDir("bar", 0700),
		fstest.CreateFile("bar/Dockerfile", dockerfile, 0600),
		fstest.CreateFile("bar/super-cool.txt", []byte("super cool"), 0600),
	)
	dirDest := t.TempDir()

	git, err := gitutil.New(gitutil.WithWorkingDir(dir))
	require.NoError(t, err)
	gitutil.GitInit(git, t)
	gitutil.GitAdd(git, t, "docker-bake.hcl", "bar")
	gitutil.GitCommit(git, t, "initial commit")
	addr := gitutil.GitServeHTTP(git, t)

	out, err := bakeCmd(sb, withDir("/tmp"), withArgs(addr, "--set", "*.output=type=local,dest="+dirDest))
	require.NoError(t, err, out)

	require.FileExists(t, filepath.Join(dirDest, "super-cool.txt"))
}

func testBakeRemoteCmdContextEscapeRoot(t *testing.T, sb integration.Sandbox) {
	dirSrc := tmpdir(
		t,
		fstest.CreateFile("foo", []byte("foo"), 0600),
	)
	dirSrc, err := filepath.Abs(dirSrc)
	require.NoError(t, err)

	dirCurrent := tmpdir(t)
	dirCurrent, err = filepath.Abs(dirCurrent)
	require.NoError(t, err)

	bakefile := []byte(`
target "default" {
	context = "cwd://` + dirSrc + `"
	dockerfile-inline = <<EOT
FROM scratch
COPY foo /foo
EOT
}
`)
	dirSpec := tmpdir(
		t,
		fstest.CreateFile("docker-bake.hcl", bakefile, 0600),
	)
	dirDest := t.TempDir()

	git, err := gitutil.New(gitutil.WithWorkingDir(dirSpec))
	require.NoError(t, err)

	gitutil.GitInit(git, t)
	gitutil.GitAdd(git, t, "docker-bake.hcl")
	gitutil.GitCommit(git, t, "initial commit")
	addr := gitutil.GitServeHTTP(git, t)

	out, err := bakeCmd(
		sb,
		withDir(dirCurrent),
		withArgs(addr, "--set", "*.output=type=local,dest="+dirDest),
	)
	require.Error(t, err, out)
	require.Contains(t, out, "outside of the working directory, please set BAKE_ALLOW_REMOTE_FS_ACCESS")

	out, err = bakeCmd(
		sb,
		withDir(dirCurrent),
		withArgs(addr, "--set", "*.output=type=local,dest="+dirDest),
		withEnv("BAKE_ALLOW_REMOTE_FS_ACCESS=1"),
	)
	require.NoError(t, err, out)
	require.FileExists(t, filepath.Join(dirDest, "foo"))
}

func testBakeRemoteCmdContextEscapeRelative(t *testing.T, sb integration.Sandbox) {
	bakefile := []byte(`
target "default" {
	context = "cwd://../"
	dockerfile-inline = <<EOT
FROM scratch
COPY foo /foo
EOT
}
`)
	dirSpec := tmpdir(
		t,
		fstest.CreateFile("docker-bake.hcl", bakefile, 0600),
	)
	dirSrc := tmpdir(
		t,
		fstest.CreateFile("foo", []byte("foo"), 0600),
		fstest.CreateDir("subdir", 0700),
	)
	dirDest := t.TempDir()

	git, err := gitutil.New(gitutil.WithWorkingDir(dirSpec))
	require.NoError(t, err)

	gitutil.GitInit(git, t)
	gitutil.GitAdd(git, t, "docker-bake.hcl")
	gitutil.GitCommit(git, t, "initial commit")
	addr := gitutil.GitServeHTTP(git, t)

	out, err := bakeCmd(
		sb,
		withDir(filepath.Join(dirSrc, "subdir")),
		withArgs(addr, "--set", "*.output=type=local,dest="+dirDest),
	)
	require.Error(t, err, out)
	require.Contains(t, out, "outside of the working directory, please set BAKE_ALLOW_REMOTE_FS_ACCESS")

	out, err = bakeCmd(
		sb,
		withDir(filepath.Join(dirSrc, "subdir")),
		withArgs(addr, "--set", "*.output=type=local,dest="+dirDest),
		withEnv("BAKE_ALLOW_REMOTE_FS_ACCESS=1"),
	)
	require.NoError(t, err, out)
	require.FileExists(t, filepath.Join(dirDest, "foo"))
}

func testBakeRemoteDockerfileCwd(t *testing.T, sb integration.Sandbox) {
	bakefile := []byte(`
target "default" {
	context = "."
	dockerfile = "cwd://Dockerfile.app"
}
`)
	dockerfile := []byte(`
FROM scratch
COPY bar /bar
	`)
	dockerfileApp := []byte(`
FROM scratch
COPY foo /foo
	`)

	dirSpec := tmpdir(
		t,
		fstest.CreateFile("docker-bake.hcl", bakefile, 0600),
		fstest.CreateFile("Dockerfile", dockerfile, 0600),
		fstest.CreateFile("foo", []byte("foo"), 0600),
		fstest.CreateFile("bar", []byte("bar"), 0600),
	)
	dirSrc := tmpdir(
		t,
		fstest.CreateFile("Dockerfile.app", dockerfileApp, 0600),
	)
	dirDest := t.TempDir()

	git, err := gitutil.New(gitutil.WithWorkingDir(dirSpec))
	require.NoError(t, err)

	gitutil.GitInit(git, t)
	gitutil.GitAdd(git, t, "docker-bake.hcl")
	gitutil.GitAdd(git, t, "Dockerfile")
	gitutil.GitAdd(git, t, "foo")
	gitutil.GitAdd(git, t, "bar")
	gitutil.GitCommit(git, t, "initial commit")
	addr := gitutil.GitServeHTTP(git, t)

	out, err := bakeCmd(
		sb,
		withDir(dirSrc),
		withArgs(addr, "--set", "*.output=type=local,dest="+dirDest),
	)
	require.NoError(t, err, out)
	require.FileExists(t, filepath.Join(dirDest, "foo"))

	err = os.Remove(filepath.Join(dirSrc, "Dockerfile.app"))
	require.NoError(t, err)

	out, err = bakeCmd(
		sb,
		withDir(dirSrc),
		withArgs(addr, "--set", "*.output=type=cacheonly"),
	)
	require.Error(t, err, out)
}
