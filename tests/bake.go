package tests

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/containerd/continuity/fs/fstest"
	"github.com/docker/buildx/util/gitutil"
	"github.com/moby/buildkit/identity"
	"github.com/moby/buildkit/util/contentutil"
	"github.com/moby/buildkit/util/testutil"
	"github.com/moby/buildkit/util/testutil/integration"
	"github.com/pkg/errors"
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
	testBakeLocalMulti,
	testBakeRemote,
	testBakeRemoteCmdContext,
	testBakeRemoteLocalOverride,
	testBakeLocalCwdOverride,
	testBakeRemoteCmdContextOverride,
	testBakeRemoteContextSubdir,
	testBakeRemoteCmdContextEscapeRoot,
	testBakeRemoteCmdContextEscapeRelative,
	testBakeRemoteDockerfileCwd,
	testBakeRemoteLocalContextRemoteDockerfile,
	testBakeEmpty,
	testBakeShmSize,
	testBakeUlimits,
	testBakeRefs,
	testBakeMultiExporters,
	testBakeLoadPush,
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

	cmd := buildxCmd(sb, withDir(dir), withArgs("bake", "--progress=plain", "--set", "*.output=type=local,dest="+dirDest))
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	require.Contains(t, string(out), `#1 [internal] load local bake definitions`)
	require.Contains(t, string(out), `#1 reading docker-bake.hcl`)

	require.FileExists(t, filepath.Join(dirDest, "foo"))
}

func testBakeLocalMulti(t *testing.T, sb integration.Sandbox) {
	dockerfile := []byte(`
FROM scratch
COPY foo /foo
	`)
	bakefile := []byte(`
target "default" {
}
`)
	composefile := []byte(`
services:
  app:
    build: {}
`)

	dir := tmpdir(
		t,
		fstest.CreateFile("docker-bake.hcl", bakefile, 0600),
		fstest.CreateFile("compose.yaml", composefile, 0600),
		fstest.CreateFile("Dockerfile", dockerfile, 0600),
		fstest.CreateFile("foo", []byte("foo"), 0600),
	)

	dirDest := t.TempDir()

	cmd := buildxCmd(sb, withDir(dir), withArgs("bake", "--progress=plain", "--set", "*.output=type=local,dest="+dirDest))
	dt, err := cmd.CombinedOutput()
	require.NoError(t, err, string(dt))
	require.Contains(t, string(dt), `#1 [internal] load local bake definitions`)
	require.Contains(t, string(dt), `#1 reading compose.yaml`)
	require.Contains(t, string(dt), `#1 reading docker-bake.hcl`)
	require.FileExists(t, filepath.Join(dirDest, "foo"))

	dirDest2 := t.TempDir()

	out, err := bakeCmd(sb, withDir(dir), withArgs("--file", "cwd://docker-bake.hcl", "--set", "*.output=type=local,dest="+dirDest2))
	require.NoError(t, err, out)

	require.FileExists(t, filepath.Join(dirDest2, "foo"))
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

func testBakeRemoteLocalOverride(t *testing.T, sb integration.Sandbox) {
	remoteBakefile := []byte(`
target "default" {
	dockerfile-inline = <<EOT
FROM scratch
COPY foo /foo
EOT
}
`)
	localBakefile := []byte(`
target "default" {
	dockerfile-inline = <<EOT
FROM scratch
COPY bar /bar
EOT
}
`)
	dirSpec := tmpdir(
		t,
		fstest.CreateFile("docker-bake.hcl", remoteBakefile, 0600),
		fstest.CreateFile("bar", []byte("bar"), 0600),
	)
	dirSrc := tmpdir(
		t,
		fstest.CreateFile("local-docker-bake.hcl", localBakefile, 0600),
	)
	dirDest := t.TempDir()

	git, err := gitutil.New(gitutil.WithWorkingDir(dirSpec))
	require.NoError(t, err)

	gitutil.GitInit(git, t)
	gitutil.GitAdd(git, t, "docker-bake.hcl", "bar")
	gitutil.GitCommit(git, t, "initial commit")
	addr := gitutil.GitServeHTTP(git, t)

	out, err := bakeCmd(sb, withDir(dirSrc), withArgs(addr, "--file", "cwd://local-docker-bake.hcl", "--set", "*.output=type=local,dest="+dirDest))
	require.NoError(t, err, out)

	require.FileExists(t, filepath.Join(dirDest, "bar"))
}

func testBakeLocalCwdOverride(t *testing.T, sb integration.Sandbox) {
	bakeFile := []byte(`
target "default" {
	dockerfile-inline = <<EOT
FROM scratch
COPY foo /foo
EOT
}
`)
	cwdBakefile := []byte(`
target "default" {
	dockerfile-inline = <<EOT
FROM scratch
COPY bar /bar
EOT
}
`)

	dir := tmpdir(
		t,
		fstest.CreateFile("docker-bake.hcl", bakeFile, 0600),
		fstest.CreateFile("docker-bake-cwd.hcl", cwdBakefile, 0600),
		fstest.CreateFile("bar", []byte("bar"), 0600),
	)
	dirDest := t.TempDir()

	cmd := buildxCmd(sb, withDir(dir), withArgs("bake", "--file", "docker-bake.hcl", "--file", "cwd://docker-bake-cwd.hcl", "--progress=plain", "--set", "*.output=type=local,dest="+dirDest))
	dt, err := cmd.CombinedOutput()
	require.NoError(t, err, string(dt))
	require.Contains(t, string(dt), `#1 [internal] load local bake definitions`)
	require.Contains(t, string(dt), `#1 reading docker-bake.hcl`)
	require.Contains(t, string(dt), `#1 reading docker-bake-cwd.hcl`)
	require.FileExists(t, filepath.Join(dirDest, "bar"))
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

func testBakeRemoteLocalContextRemoteDockerfile(t *testing.T, sb integration.Sandbox) {
	bakefile := []byte(`
target "default" {
	context = BAKE_CMD_CONTEXT
	dockerfile = "Dockerfile.app"
}
`)
	dockerfileApp := []byte(`
FROM scratch
COPY foo /foo
	`)

	dirSpec := tmpdir(
		t,
		fstest.CreateFile("docker-bake.hcl", bakefile, 0600),
	)
	dirSrc := tmpdir(
		t,
		fstest.CreateFile("Dockerfile.app", dockerfileApp, 0600),
		fstest.CreateFile("foo", []byte("foo"), 0600),
	)

	git, err := gitutil.New(gitutil.WithWorkingDir(dirSpec))
	require.NoError(t, err)

	gitutil.GitInit(git, t)
	gitutil.GitAdd(git, t, "docker-bake.hcl")
	gitutil.GitCommit(git, t, "initial commit")
	addr := gitutil.GitServeHTTP(git, t)

	out, err := bakeCmd(
		sb,
		withDir(dirSrc),
		withArgs(addr, "--set", "*.output=type=cacheonly"),
	)
	require.Error(t, err, out)
	require.Contains(t, out, "reading a dockerfile for a remote build invocation is currently not supported")
}

func testBakeEmpty(t *testing.T, sb integration.Sandbox) {
	out, err := bakeCmd(sb)
	require.Error(t, err, out)
	require.Contains(t, out, "couldn't find a bake definition")
}

func testBakeShmSize(t *testing.T, sb integration.Sandbox) {
	dockerfile := []byte(`
FROM busybox AS build
RUN mount | grep /dev/shm > /shmsize
FROM scratch
COPY --from=build /shmsize /
	`)
	bakefile := []byte(`
target "default" {
  shm-size = "128m"
}
`)
	dir := tmpdir(
		t,
		fstest.CreateFile("docker-bake.hcl", bakefile, 0600),
		fstest.CreateFile("Dockerfile", dockerfile, 0600),
	)

	dirDest := t.TempDir()

	out, err := bakeCmd(
		sb,
		withDir(dir),
		withArgs("--set", "*.output=type=local,dest="+dirDest),
	)
	require.NoError(t, err, out)

	dt, err := os.ReadFile(filepath.Join(dirDest, "shmsize"))
	require.NoError(t, err)
	require.Contains(t, string(dt), `size=131072k`)
}

func testBakeUlimits(t *testing.T, sb integration.Sandbox) {
	dockerfile := []byte(`
FROM busybox AS build
RUN ulimit -n > first > /ulimit
FROM scratch
COPY --from=build /ulimit /
	`)
	bakefile := []byte(`
target "default" {
  ulimits = ["nofile=1024:1024"]
}
`)
	dir := tmpdir(
		t,
		fstest.CreateFile("docker-bake.hcl", bakefile, 0600),
		fstest.CreateFile("Dockerfile", dockerfile, 0600),
	)

	dirDest := t.TempDir()

	out, err := bakeCmd(
		sb,
		withDir(dir),
		withArgs("--set", "*.output=type=local,dest="+dirDest),
	)
	require.NoError(t, err, out)

	dt, err := os.ReadFile(filepath.Join(dirDest, "ulimit"))
	require.NoError(t, err)
	require.Contains(t, string(dt), `1024`)
}

func testBakeRefs(t *testing.T, sb integration.Sandbox) {
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

	outFlag := "default.output=type=docker"
	if sb.DockerAddress() == "" {
		// there is no Docker atm to load the image
		outFlag += ",dest=" + dirDest + "/image.tar"
	}

	cmd := buildxCmd(sb, withDir(dir), withArgs("bake", "--metadata-file", filepath.Join(dirDest, "md.json"), "--set", outFlag))
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, out)

	dt, err := os.ReadFile(filepath.Join(dirDest, "md.json"))
	require.NoError(t, err)

	type mdT struct {
		Default struct {
			BuildRef string `json:"buildx.build.ref"`
		} `json:"default"`
	}
	var md mdT
	err = json.Unmarshal(dt, &md)
	require.NoError(t, err)

	require.NotEmpty(t, md.Default.BuildRef)
}

func testBakeMultiExporters(t *testing.T, sb integration.Sandbox) {
	if sb.Name() != "docker" {
		t.Skip("skipping test for non-docker workers")
	}

	registry, err := sb.NewRegistry()
	if errors.Is(err, integration.ErrRequirements) {
		t.Skip(err.Error())
	}
	require.NoError(t, err)

	targetReg := registry + "/buildx/registry:latest"
	targetStore := "buildx:local-" + identity.NewID()

	var builderName string
	t.Cleanup(func() {
		if builderName == "" {
			return
		}

		cmd := dockerCmd(sb, withArgs("image", "rm", targetStore))
		cmd.Stderr = os.Stderr
		require.NoError(t, cmd.Run())

		out, err := rmCmd(sb, withArgs(builderName))
		require.NoError(t, err, out)
	})

	// TODO: use stable buildkit image when v0.13.0 released
	out, err := createCmd(sb, withArgs(
		"--driver", "docker-container",
		"--buildkitd-flags=--allow-insecure-entitlement=network.host",
		"--driver-opt", "network=host",
		"--driver-opt", "image=moby/buildkit:v0.13.0-rc3",
	))
	require.NoError(t, err, out)
	builderName = strings.TrimSpace(out)

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

	outputs := []string{
		"--set", fmt.Sprintf("*.output=type=image,name=%s,push=true", targetReg),
		"--set", fmt.Sprintf("*.output=type=docker,name=%s", targetStore),
		"--set", fmt.Sprintf("*.output=type=oci,dest=%s/result", dir),
	}
	cmd := buildxCmd(sb, withDir(dir), withArgs("bake"), withArgs(outputs...))
	cmd.Env = append(cmd.Env, "BUILDX_BUILDER="+builderName)
	outb, err := cmd.CombinedOutput()
	require.NoError(t, err, string(outb))

	// test registry
	desc, provider, err := contentutil.ProviderFromRef(targetReg)
	require.NoError(t, err)
	_, err = testutil.ReadImages(sb.Context(), provider, desc)
	require.NoError(t, err)

	// test docker store
	cmd = dockerCmd(sb, withArgs("image", "inspect", targetStore))
	cmd.Stderr = os.Stderr
	require.NoError(t, cmd.Run())

	// test oci
	_, err = os.ReadFile(fmt.Sprintf("%s/result", dir))
	require.NoError(t, err)

	// TODO: test metadata file when supported by multi exporters https://github.com/docker/buildx/issues/2181
}

func testBakeLoadPush(t *testing.T, sb integration.Sandbox) {
	if sb.Name() != "docker" {
		t.Skip("skipping test for non-docker workers")
	}

	registry, err := sb.NewRegistry()
	if errors.Is(err, integration.ErrRequirements) {
		t.Skip(err.Error())
	}
	require.NoError(t, err)

	target := registry + "/buildx/registry:" + identity.NewID()

	var builderName string
	t.Cleanup(func() {
		if builderName == "" {
			return
		}

		cmd := dockerCmd(sb, withArgs("image", "rm", target))
		cmd.Stderr = os.Stderr
		require.NoError(t, cmd.Run())

		out, err := rmCmd(sb, withArgs(builderName))
		require.NoError(t, err, out)
	})

	// TODO: use stable buildkit image when v0.13.0 released
	out, err := createCmd(sb, withArgs(
		"--driver", "docker-container",
		"--buildkitd-flags=--allow-insecure-entitlement=network.host",
		"--driver-opt", "network=host",
		"--driver-opt", "image=moby/buildkit:v0.13.0-rc3",
	))
	require.NoError(t, err, out)
	builderName = strings.TrimSpace(out)

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

	cmd := buildxCmd(sb, withDir(dir), withArgs("bake", "--push", "--load", fmt.Sprintf("--set=*.tags=%s", target)))
	cmd.Env = append(cmd.Env, "BUILDX_BUILDER="+builderName)
	outb, err := cmd.CombinedOutput()
	require.NoError(t, err, string(outb))

	// test registry
	desc, provider, err := contentutil.ProviderFromRef(target)
	require.NoError(t, err)
	_, err = testutil.ReadImages(sb.Context(), provider, desc)
	require.NoError(t, err)

	// test docker store
	cmd = dockerCmd(sb, withArgs("image", "inspect", target))
	cmd.Stderr = os.Stderr
	require.NoError(t, cmd.Run())

	// TODO: test metadata file when supported by multi exporters https://github.com/docker/buildx/issues/2181
}
