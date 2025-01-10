package tests

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/containerd/continuity/fs/fstest"
	"github.com/docker/buildx/bake"
	"github.com/docker/buildx/util/gitutil"
	"github.com/moby/buildkit/client"
	"github.com/moby/buildkit/frontend/subrequests/lint"
	"github.com/moby/buildkit/identity"
	provenancetypes "github.com/moby/buildkit/solver/llbsolver/provenance/types"
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
	testBakePrint,
	testBakePrintSensitive,
	testBakeLocal,
	testBakeLocalMulti,
	testBakeRemote,
	testBakeRemoteAuth,
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
	testBakeSetNonExistingSubdirNoParallel,
	testBakeSetNonExistingOutsideNoParallel,
	testBakeSetExistingOutsideNoParallel,
	testBakeDefinitionNotExistingSubdirNoParallel,
	testBakeDefinitionNotExistingOutsideNoParallel,
	testBakeDefinitionExistingOutsideNoParallel,
	testBakeDefinitionSymlinkOutsideNoParallel,
	testBakeDefinitionSymlinkOutsideGrantedNoParallel,
	testBakeSSHPathNoParallel,
	testBakeSSHDefaultNoParallel,
	testBakeShmSize,
	testBakeUlimits,
	testBakeMetadataProvenance,
	testBakeMetadataWarnings,
	testBakeMetadataWarningsDedup,
	testBakeMultiExporters,
	testBakeLoadPush,
	testListTargets,
	testListVariables,
	testBakeCallCheck,
	testBakeCallCheckFlag,
	testBakeCallMetadata,
	testBakeMultiPlatform,
	testBakeCheckCallOutput,
}

func testBakePrint(t *testing.T, sb integration.Sandbox) {
	testCases := []struct {
		name string
		f    string
		dt   []byte
	}{
		{
			"HCL",
			"docker-bake.hcl",
			[]byte(`
target "build" {
  args = {
    HELLO = "foo"
  }
}
`),
		},
		{
			"Compose",
			"compose.yml",
			[]byte(`
services:
  build:
    build:
      context: .
      args:
        HELLO: foo
`),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			dir := tmpdir(
				t,
				fstest.CreateFile(tc.f, tc.dt, 0600),
				fstest.CreateFile("Dockerfile", []byte(`
FROM busybox
ARG HELLO
RUN echo "Hello ${HELLO}"
	`), 0600),
			)

			cmd := buildxCmd(sb, withDir(dir), withArgs("bake", "--print", "build"))
			stdout := bytes.Buffer{}
			stderr := bytes.Buffer{}
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr
			require.NoError(t, cmd.Run(), stdout.String(), stderr.String())

			var def struct {
				Group  map[string]*bake.Group  `json:"group,omitempty"`
				Target map[string]*bake.Target `json:"target"`
			}
			require.NoError(t, json.Unmarshal(stdout.Bytes(), &def))

			require.Len(t, def.Group, 1)
			require.Contains(t, def.Group, "default")

			require.Equal(t, []string{"build"}, def.Group["default"].Targets)
			require.Len(t, def.Target, 1)
			require.Contains(t, def.Target, "build")
			require.Equal(t, ".", *def.Target["build"].Context)
			require.Equal(t, "Dockerfile", *def.Target["build"].Dockerfile)
			require.Equal(t, map[string]*string{"HELLO": ptrstr("foo")}, def.Target["build"].Args)

			require.JSONEq(t, `{
  "group": {
    "default": {
      "targets": [
        "build"
      ]
    }
  },
  "target": {
    "build": {
      "context": ".",
      "dockerfile": "Dockerfile",
      "args": {
        "HELLO": "foo"
      }
    }
  }
}
`, stdout.String())
		})
	}
}

func testBakePrintSensitive(t *testing.T, sb integration.Sandbox) {
	testCases := []struct {
		name string
		f    string
		dt   []byte
	}{
		{
			"HCL",
			"docker-bake.hcl",
			[]byte(`
target "build" {
  args = {
    HELLO = "foo"
  }

  cache-from = [
    "type=gha,token=abc",
    "type=s3,region=us-west-2,bucket=my_bucket,name=my_image",
  ]
}
`),
		},
		{
			"Compose",
			"compose.yml",
			[]byte(`
services:
  build:
    build:
      context: .
      args:
        HELLO: foo
      cache_from:
        - type=gha,token=abc
        - type=s3,region=us-west-2,bucket=my_bucket,name=my_image
`),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			dir := tmpdir(
				t,
				fstest.CreateFile(tc.f, tc.dt, 0600),
				fstest.CreateFile("Dockerfile", []byte(`
FROM busybox
ARG HELLO
RUN echo "Hello ${HELLO}"
	`), 0600),
			)

			cmd := buildxCmd(sb, withDir(dir), withArgs("bake", "--print", "build"),
				withEnv(
					"ACTIONS_RUNTIME_TOKEN=sensitive_token",
					"ACTIONS_CACHE_URL=https://cache.github.com",
					"AWS_ACCESS_KEY_ID=definitely_dont_look_here",
					"AWS_SECRET_ACCESS_KEY=hackers_please_dont_steal",
					"AWS_SESSION_TOKEN=not_a_mitm_attack",
				),
			)
			stdout := bytes.Buffer{}
			stderr := bytes.Buffer{}
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr
			require.NoError(t, cmd.Run(), stdout.String(), stderr.String())

			var def struct {
				Group  map[string]*bake.Group  `json:"group,omitempty"`
				Target map[string]*bake.Target `json:"target"`
			}
			require.NoError(t, json.Unmarshal(stdout.Bytes(), &def))

			require.Len(t, def.Group, 1)
			require.Contains(t, def.Group, "default")

			require.Equal(t, []string{"build"}, def.Group["default"].Targets)
			require.Len(t, def.Target, 1)
			require.Contains(t, def.Target, "build")
			require.Equal(t, ".", *def.Target["build"].Context)
			require.Equal(t, "Dockerfile", *def.Target["build"].Dockerfile)
			require.Equal(t, map[string]*string{"HELLO": ptrstr("foo")}, def.Target["build"].Args)
			require.NotNil(t, def.Target["build"].CacheFrom)
			require.Len(t, def.Target["build"].CacheFrom, 2)

			require.JSONEq(t, `{
  "group": {
    "default": {
      "targets": [
        "build"
      ]
    }
  },
  "target": {
    "build": {
      "context": ".",
      "dockerfile": "Dockerfile",
      "args": {
        "HELLO": "foo"
      },
      "cache-from": [
        {
          "type": "gha",
          "token": "abc"
        },
        {
          "type": "s3",
          "region": "us-west-2",
          "bucket": "my_bucket",
          "name": "my_image"
        }
      ]
    }
  }
}
`, stdout.String())
		})
	}
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

func testBakeRemoteAuth(t *testing.T, sb integration.Sandbox) {
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

	token := identity.NewID()
	addr := gitutil.GitServeHTTP(git, t, gitutil.WithAccessToken(token))

	out, err := bakeCmd(sb, withDir(dir),
		withEnv("BUILDX_BAKE_GIT_AUTH_TOKEN="+token),
		withArgs(addr, "--set", "*.output=type=local,dest="+dirDest),
	)
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
	require.Contains(t, out, "Your build is requesting privileges for following possibly insecure capabilities")
	require.Contains(t, out, "Read access to path ../")

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
	require.Contains(t, out, "Your build is requesting privileges for following possibly insecure capabilities")
	require.Contains(t, out, "Read access to path ..")

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

func testBakeSetNonExistingSubdirNoParallel(t *testing.T, sb integration.Sandbox) {
	for _, ent := range []bool{true, false} {
		t.Run(fmt.Sprintf("ent=%v", ent), func(t *testing.T) {
			t.Setenv("BUILDX_BAKE_ENTITLEMENTS_FS", strconv.FormatBool(ent))
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

			cmd := buildxCmd(sb, withDir(dir), withArgs("bake", "--progress=plain", "--set", "*.output=type=local,dest="+filepath.Join(dir, "not/exists")))
			out, err := cmd.CombinedOutput()
			require.NoError(t, err, string(out))
			require.Contains(t, string(out), `#1 [internal] load local bake definitions`)
			require.Contains(t, string(out), `#1 reading docker-bake.hcl`)

			require.FileExists(t, filepath.Join(dir, "not/exists/foo"))
		})
	}
}
func testBakeSetNonExistingOutsideNoParallel(t *testing.T, sb integration.Sandbox) {
	for _, ent := range []bool{true, false} {
		t.Run(fmt.Sprintf("ent=%v", ent), func(t *testing.T) {
			t.Setenv("BUILDX_BAKE_ENTITLEMENTS_FS", strconv.FormatBool(ent))
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

			destDir := t.TempDir()

			cmd := buildxCmd(sb, withDir(dir), withArgs("bake", "--progress=plain", "--set", "*.output=type=local,dest="+filepath.Join(destDir, "not/exists")))
			out, err := cmd.CombinedOutput()
			if ent {
				require.Error(t, err, string(out))
				require.Contains(t, string(out), "ERROR: additional privileges requested")
			} else {
				require.NoError(t, err, string(out))
				require.FileExists(t, filepath.Join(destDir, "not/exists/foo"))
			}
		})
	}
}

func testBakeSetExistingOutsideNoParallel(t *testing.T, sb integration.Sandbox) {
	for _, ent := range []bool{true, false} {
		t.Run(fmt.Sprintf("ent=%v", ent), func(t *testing.T) {
			t.Setenv("BUILDX_BAKE_ENTITLEMENTS_FS", strconv.FormatBool(ent))
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

			destDir := t.TempDir()

			cmd := buildxCmd(sb, withDir(dir), withArgs("bake", "--progress=plain", "--set", "*.output=type=local,dest="+destDir))
			out, err := cmd.CombinedOutput()
			// existing directory via --set is always allowed
			require.NoError(t, err, string(out))
			require.FileExists(t, filepath.Join(destDir, "foo"))
		})
	}
}

func testBakeDefinitionNotExistingSubdirNoParallel(t *testing.T, sb integration.Sandbox) {
	for _, ent := range []bool{true, false} {
		t.Run(fmt.Sprintf("ent=%v", ent), func(t *testing.T) {
			t.Setenv("BUILDX_BAKE_ENTITLEMENTS_FS", strconv.FormatBool(ent))
			dockerfile := []byte(`
FROM scratch
COPY foo /foo
	`)
			bakefile := []byte(`
target "default" {
	output = ["type=local,dest=not/exists"]
}
`)
			dir := tmpdir(
				t,
				fstest.CreateFile("docker-bake.hcl", bakefile, 0600),
				fstest.CreateFile("Dockerfile", dockerfile, 0600),
				fstest.CreateFile("foo", []byte("foo"), 0600),
			)

			cmd := buildxCmd(sb, withDir(dir), withArgs("bake", "--progress=plain"))
			out, err := cmd.CombinedOutput()
			// subdirs of working directory are always allowed
			require.NoError(t, err, string(out))
			require.FileExists(t, filepath.Join(dir, "not/exists/foo"))
		})
	}
}

func testBakeDefinitionNotExistingOutsideNoParallel(t *testing.T, sb integration.Sandbox) {
	for _, ent := range []bool{true, false} {
		t.Run(fmt.Sprintf("ent=%v", ent), func(t *testing.T) {
			t.Setenv("BUILDX_BAKE_ENTITLEMENTS_FS", strconv.FormatBool(ent))
			dockerfile := []byte(`
FROM scratch
COPY foo /foo
	`)
			destDir := t.TempDir()
			bakefile := []byte(fmt.Sprintf(`
target "default" {
	output = ["type=local,dest=%s/not/exists"]
}
`, destDir))
			dir := tmpdir(
				t,
				fstest.CreateFile("docker-bake.hcl", bakefile, 0600),
				fstest.CreateFile("Dockerfile", dockerfile, 0600),
				fstest.CreateFile("foo", []byte("foo"), 0600),
			)

			cmd := buildxCmd(sb, withDir(dir), withArgs("bake", "--progress=plain"))
			out, err := cmd.CombinedOutput()
			if ent {
				require.Error(t, err, string(out))
				require.Contains(t, string(out), "ERROR: additional privileges requested")
			} else {
				require.NoError(t, err, string(out))
				require.FileExists(t, filepath.Join(destDir, "not/exists/foo"))
			}
		})
	}
}

func testBakeDefinitionExistingOutsideNoParallel(t *testing.T, sb integration.Sandbox) {
	for _, ent := range []bool{true, false} {
		t.Run(fmt.Sprintf("ent=%v", ent), func(t *testing.T) {
			t.Setenv("BUILDX_BAKE_ENTITLEMENTS_FS", strconv.FormatBool(ent))
			dockerfile := []byte(`
FROM scratch
COPY foo /foo
	`)
			destDir := t.TempDir()
			bakefile := []byte(fmt.Sprintf(`
target "default" {
	output = ["type=local,dest=%s"]
}
`, destDir))
			dir := tmpdir(
				t,
				fstest.CreateFile("docker-bake.hcl", bakefile, 0600),
				fstest.CreateFile("Dockerfile", dockerfile, 0600),
				fstest.CreateFile("foo", []byte("foo"), 0600),
			)

			cmd := buildxCmd(sb, withDir(dir), withArgs("bake", "--progress=plain"))
			out, err := cmd.CombinedOutput()
			if ent {
				require.Error(t, err, string(out))
				require.Contains(t, string(out), "ERROR: additional privileges requested")
			} else {
				require.NoError(t, err, string(out))
				require.FileExists(t, filepath.Join(destDir, "foo"))
			}
		})
	}
}

func testBakeDefinitionSymlinkOutsideNoParallel(t *testing.T, sb integration.Sandbox) {
	for _, ent := range []bool{true, false} {
		t.Run(fmt.Sprintf("ent=%v", ent), func(t *testing.T) {
			t.Setenv("BUILDX_BAKE_ENTITLEMENTS_FS", strconv.FormatBool(ent))
			dockerfile := []byte(`
FROM scratch
COPY foo /foo
	`)
			destDir := t.TempDir()
			bakefile := []byte(`
target "default" {
	output = ["type=local,dest=out"]
}
`)
			dir := tmpdir(
				t,
				fstest.CreateFile("docker-bake.hcl", bakefile, 0600),
				fstest.CreateFile("Dockerfile", dockerfile, 0600),
				fstest.CreateFile("foo", []byte("foo"), 0600),
				fstest.Symlink(destDir, "out"),
			)

			cmd := buildxCmd(sb, withDir(dir), withArgs("bake", "--progress=plain"))
			out, err := cmd.CombinedOutput()
			if ent {
				require.Error(t, err, string(out))
				require.Contains(t, string(out), "ERROR: additional privileges requested")
			} else {
				require.NoError(t, err, string(out))
				require.FileExists(t, filepath.Join(destDir, "foo"))
			}
		})
	}
}

func testBakeDefinitionSymlinkOutsideGrantedNoParallel(t *testing.T, sb integration.Sandbox) {
	for _, ent := range []bool{true, false} {
		t.Run(fmt.Sprintf("ent=%v", ent), func(t *testing.T) {
			t.Setenv("BUILDX_BAKE_ENTITLEMENTS_FS", strconv.FormatBool(ent))
			dockerfile := []byte(`
FROM scratch
COPY foo /foo
	`)
			destDir := t.TempDir()
			bakefile := []byte(`
target "default" {
	output = ["type=local,dest=out"]
}
`)
			dir := tmpdir(
				t,
				fstest.CreateFile("docker-bake.hcl", bakefile, 0600),
				fstest.CreateFile("Dockerfile", dockerfile, 0600),
				fstest.CreateFile("foo", []byte("foo"), 0600),
				fstest.Symlink(destDir, "out"),
			)

			cmd := buildxCmd(sb, withDir(dir), withArgs("bake", "--progress=plain", "--allow", "fs.write="+destDir))
			out, err := cmd.CombinedOutput()
			require.NoError(t, err, string(out))
			require.FileExists(t, filepath.Join(destDir, "foo"))
		})
	}
}

func testBakeSSHPathNoParallel(t *testing.T, sb integration.Sandbox) {
	for _, ent := range []bool{true, false} {
		t.Run(fmt.Sprintf("ent=%v", ent), func(t *testing.T) {
			t.Setenv("BUILDX_BAKE_ENTITLEMENTS_FS", strconv.FormatBool(ent))
			dockerfile := []byte(`
FROM scratch
COPY Dockerfile /foo
	`)
			keyDir := t.TempDir()
			err := writeTempPrivateKey(filepath.Join(keyDir, "id_rsa"))
			require.NoError(t, err)
			bakefile := []byte(fmt.Sprintf(`
target "default" {
	ssh = ["key=%s"]
}
`, filepath.Join(keyDir, "id_rsa")))
			dir := tmpdir(
				t,
				fstest.CreateFile("docker-bake.hcl", bakefile, 0600),
				fstest.CreateFile("Dockerfile", dockerfile, 0600),
			)

			// not allowed
			cmd := buildxCmd(sb, withDir(dir), withArgs("bake", "--progress=plain"))
			out, err := cmd.CombinedOutput()
			if ent {
				require.Error(t, err, string(out))
				require.Contains(t, string(out), "ERROR: additional privileges requested")
				require.Contains(t, string(out), "Read access to path")
				require.Contains(t, string(out), "/id_rsa")
			} else {
				require.NoError(t, err, string(out))
			}

			// directory allowed
			cmd = buildxCmd(sb, withDir(dir), withArgs("bake", "--progress=plain", "--allow", "fs.read="+keyDir))
			out, err = cmd.CombinedOutput()
			require.NoError(t, err, string(out))

			// file allowed
			cmd = buildxCmd(sb, withDir(dir), withArgs("bake", "--progress=plain", "--allow", "fs.read="+filepath.Join(keyDir, "id_rsa")))
			out, err = cmd.CombinedOutput()
			require.NoError(t, err, string(out))
		})
	}
}

func testBakeSSHDefaultNoParallel(t *testing.T, sb integration.Sandbox) {
	for _, ent := range []bool{true, false} {
		t.Run(fmt.Sprintf("ent=%v", ent), func(t *testing.T) {
			t.Setenv("BUILDX_BAKE_ENTITLEMENTS_FS", strconv.FormatBool(ent))
			dockerfile := []byte(`
FROM scratch
COPY Dockerfile /foo
	`)
			keyDir := t.TempDir()
			// not a socket but key behaves the same and doesn't create parse error
			err := writeTempPrivateKey(filepath.Join(keyDir, "ssh-agent.sock"))
			require.NoError(t, err)
			t.Setenv("SSH_AUTH_SOCK", filepath.Join(keyDir, "ssh-agent.sock"))
			bakefile := []byte(`
target "default" {
	ssh = ["default"]
}
`)
			dir := tmpdir(
				t,
				fstest.CreateFile("docker-bake.hcl", bakefile, 0600),
				fstest.CreateFile("Dockerfile", dockerfile, 0600),
			)

			// not allowed
			cmd := buildxCmd(sb, withDir(dir), withArgs("bake", "--progress=plain"))
			out, err := cmd.CombinedOutput()
			if ent {
				require.Error(t, err, string(out))
				require.Contains(t, string(out), "ERROR: additional privileges requested")
				require.Contains(t, string(out), "Forwarding default SSH agent socket")
			} else {
				require.NoError(t, err, string(out))
			}

			cmd = buildxCmd(sb, withDir(dir), withArgs("bake", "--progress=plain", "--allow=ssh"))
			out, err = cmd.CombinedOutput()
			require.NoError(t, err, string(out))
		})
	}
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

func testBakeMetadataProvenance(t *testing.T, sb integration.Sandbox) {
	t.Run("default", func(t *testing.T) {
		bakeMetadataProvenance(t, sb, "")
	})
	t.Run("max", func(t *testing.T) {
		bakeMetadataProvenance(t, sb, "max")
	})
	t.Run("min", func(t *testing.T) {
		bakeMetadataProvenance(t, sb, "min")
	})
	t.Run("disabled", func(t *testing.T) {
		bakeMetadataProvenance(t, sb, "disabled")
	})
}

func bakeMetadataProvenance(t *testing.T, sb integration.Sandbox, metadataMode string) {
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

	cmd := buildxCmd(
		sb,
		withDir(dir),
		withArgs("bake", "--metadata-file", filepath.Join(dirDest, "md.json"), "--set", outFlag),
		withEnv("BUILDX_METADATA_PROVENANCE="+metadataMode),
	)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))

	dt, err := os.ReadFile(filepath.Join(dirDest, "md.json"))
	require.NoError(t, err)

	type mdT struct {
		Default struct {
			BuildRef        string                 `json:"buildx.build.ref"`
			BuildProvenance map[string]interface{} `json:"buildx.build.provenance"`
		} `json:"default"`
	}
	var md mdT
	err = json.Unmarshal(dt, &md)
	require.NoError(t, err)

	require.NotEmpty(t, md.Default.BuildRef)
	if metadataMode == "disabled" {
		require.Empty(t, md.Default.BuildProvenance)
		return
	}
	require.NotEmpty(t, md.Default.BuildProvenance)

	dtprv, err := json.Marshal(md.Default.BuildProvenance)
	require.NoError(t, err)

	var prv provenancetypes.ProvenancePredicate
	require.NoError(t, json.Unmarshal(dtprv, &prv))
	require.Equal(t, provenancetypes.BuildKitBuildType, prv.BuildType)
}

func testBakeMetadataWarnings(t *testing.T, sb integration.Sandbox) {
	t.Run("default", func(t *testing.T) {
		bakeMetadataWarnings(t, sb, "")
	})
	t.Run("true", func(t *testing.T) {
		bakeMetadataWarnings(t, sb, "true")
	})
	t.Run("false", func(t *testing.T) {
		bakeMetadataWarnings(t, sb, "false")
	})
}

func bakeMetadataWarnings(t *testing.T, sb integration.Sandbox, mode string) {
	dockerfile := []byte(`
frOM busybox as base
cOpy Dockerfile .
from scratch
COPy --from=base \
  /Dockerfile \
  /
	`)
	bakefile := []byte(`
target "default" {
}
`)
	dir := tmpdir(
		t,
		fstest.CreateFile("docker-bake.hcl", bakefile, 0600),
		fstest.CreateFile("Dockerfile", dockerfile, 0600),
	)

	dirDest := t.TempDir()

	cmd := buildxCmd(
		sb,
		withDir(dir),
		withArgs("bake", "--metadata-file", filepath.Join(dirDest, "md.json"), "--set", "*.output=type=cacheonly"),
		withEnv("BUILDX_METADATA_WARNINGS="+mode),
	)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))

	dt, err := os.ReadFile(filepath.Join(dirDest, "md.json"))
	require.NoError(t, err)

	type mdT struct {
		BuildWarnings []client.VertexWarning `json:"buildx.build.warnings"`
		Default       struct {
			BuildRef string `json:"buildx.build.ref"`
		} `json:"default"`
	}
	var md mdT
	err = json.Unmarshal(dt, &md)
	require.NoError(t, err, string(dt))

	require.NotEmpty(t, md.Default.BuildRef, string(dt))
	if mode == "" || mode == "false" {
		require.Empty(t, md.BuildWarnings, string(dt))
		return
	}

	skipNoCompatBuildKit(t, sb, ">= 0.14.0-0", "lint")
	require.Len(t, md.BuildWarnings, 3, string(dt))
}

func testBakeMetadataWarningsDedup(t *testing.T, sb integration.Sandbox) {
	dockerfile := []byte(`
frOM busybox as base
cOpy Dockerfile .
from scratch
COPy --from=base \
  /Dockerfile \
  /
	`)
	bakefile := []byte(`
group "default" {
  targets = ["base", "def"]
}
target "base" {
  target = "base"
}
target "def" {
}
`)
	dir := tmpdir(
		t,
		fstest.CreateFile("docker-bake.hcl", bakefile, 0600),
		fstest.CreateFile("Dockerfile", dockerfile, 0600),
	)

	dirDest := t.TempDir()

	cmd := buildxCmd(
		sb,
		withDir(dir),
		withArgs("bake", "--metadata-file", filepath.Join(dirDest, "md.json"), "--set", "*.output=type=cacheonly"),
		withEnv("BUILDX_METADATA_WARNINGS=true"),
	)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))

	dt, err := os.ReadFile(filepath.Join(dirDest, "md.json"))
	require.NoError(t, err)

	type mdT struct {
		BuildWarnings []client.VertexWarning `json:"buildx.build.warnings"`
		Base          struct {
			BuildRef string `json:"buildx.build.ref"`
		} `json:"base"`
		Def struct {
			BuildRef string `json:"buildx.build.ref"`
		} `json:"def"`
	}
	var md mdT
	err = json.Unmarshal(dt, &md)
	require.NoError(t, err, string(dt))

	require.NotEmpty(t, md.Base.BuildRef, string(dt))
	require.NotEmpty(t, md.Def.BuildRef, string(dt))

	skipNoCompatBuildKit(t, sb, ">= 0.14.0-0", "lint")
	require.Len(t, md.BuildWarnings, 3, string(dt))
}

func testBakeMultiPlatform(t *testing.T, sb integration.Sandbox) {
	registry, err := sb.NewRegistry()
	if errors.Is(err, integration.ErrRequirements) {
		t.Skip(err.Error())
	}
	require.NoError(t, err)
	target := registry + "/buildx/registry:latest"

	dockerfile := []byte(`
	FROM --platform=$BUILDPLATFORM busybox:latest AS base
	COPY foo /etc/foo
	RUN cp /etc/foo /etc/bar

	FROM scratch
	COPY --from=base /etc/bar /bar
	`)
	bakefile := []byte(`
	target "default" {
	platforms = ["linux/amd64", "linux/arm64"]
	}
	`)
	dir := tmpdir(
		t,
		fstest.CreateFile("docker-bake.hcl", bakefile, 0600),
		fstest.CreateFile("Dockerfile", dockerfile, 0600),
		fstest.CreateFile("foo", []byte("foo"), 0600),
	)

	cmd := buildxCmd(sb, withDir(dir), withArgs("bake"), withArgs("--set", fmt.Sprintf("*.output=type=image,name=%s,push=true", target)))
	out, err := cmd.CombinedOutput()

	if !isMobyWorker(sb) {
		require.NoError(t, err, string(out))

		desc, provider, err := contentutil.ProviderFromRef(target)
		require.NoError(t, err)
		imgs, err := testutil.ReadImages(sb.Context(), provider, desc)
		require.NoError(t, err)

		img := imgs.Find("linux/amd64")
		require.NotNil(t, img)
		img = imgs.Find("linux/arm64")
		require.NotNil(t, img)
	} else {
		require.Error(t, err, string(out))
		require.Contains(t, string(out), "Multi-platform build is not supported")
	}
}

func testBakeMultiExporters(t *testing.T, sb integration.Sandbox) {
	if !isDockerContainerWorker(sb) {
		t.Skip("only testing with docker-container worker")
	}
	skipNoCompatBuildKit(t, sb, ">= 0.13.0-0", "multi exporters")

	registry, err := sb.NewRegistry()
	if errors.Is(err, integration.ErrRequirements) {
		t.Skip(err.Error())
	}
	require.NoError(t, err)

	targetReg := registry + "/buildx/registry:latest"
	targetStore := "buildx:local-" + identity.NewID()

	t.Cleanup(func() {
		cmd := dockerCmd(sb, withArgs("image", "rm", targetStore))
		cmd.Stderr = os.Stderr
		require.NoError(t, cmd.Run())
	})

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
	if !isDockerContainerWorker(sb) {
		t.Skip("only testing with docker-container worker")
	}
	skipNoCompatBuildKit(t, sb, ">= 0.13.0-0", "multi exporters")

	registry, err := sb.NewRegistry()
	if errors.Is(err, integration.ErrRequirements) {
		t.Skip(err.Error())
	}
	require.NoError(t, err)

	target := registry + "/buildx/registry:" + identity.NewID()

	t.Cleanup(func() {
		cmd := dockerCmd(sb, withArgs("image", "rm", target))
		cmd.Stderr = os.Stderr
		require.NoError(t, cmd.Run())
	})

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

func testListTargets(t *testing.T, sb integration.Sandbox) {
	bakefile := []byte(`
target "foo" {
	description = "This builds foo"
}
target "abc" {
}
`)
	dir := tmpdir(
		t,
		fstest.CreateFile("docker-bake.hcl", bakefile, 0600),
	)

	out, err := bakeCmd(
		sb,
		withDir(dir),
		withArgs("--list=targets"),
	)
	require.NoError(t, err, out)

	require.Equal(t, "TARGET\tDESCRIPTION\nabc\t\nfoo\tThis builds foo", strings.TrimSpace(out))
}

func testListVariables(t *testing.T, sb integration.Sandbox) {
	bakefile := []byte(`
variable "foo" {
	default = "bar"
	description = "This is foo"
}
variable "abc" {
	default = null
}
variable "def" {
}
target "default" {
}
`)
	dir := tmpdir(
		t,
		fstest.CreateFile("docker-bake.hcl", bakefile, 0600),
	)

	out, err := bakeCmd(
		sb,
		withDir(dir),
		withArgs("--list=variables"),
	)
	require.NoError(t, err, out)

	require.Equal(t, "VARIABLE\tVALUE\tDESCRIPTION\nabc\t\t<null>\t\ndef\t\t\t\nfoo\t\tbar\tThis is foo", strings.TrimSpace(out))
}

func testBakeCallCheck(t *testing.T, sb integration.Sandbox) {
	dockerfile := []byte(`
FROM scratch
COPy foo /foo
	`)
	bakefile := []byte(`
target "validate" {
	call = "check"
}
`)
	dir := tmpdir(
		t,
		fstest.CreateFile("docker-bake.hcl", bakefile, 0600),
		fstest.CreateFile("Dockerfile", dockerfile, 0600),
	)

	out, err := bakeCmd(
		sb,
		withDir(dir),
		withArgs("validate"),
	)
	require.Error(t, err, out)

	require.Contains(t, out, "validate")
	require.Contains(t, out, "ConsistentInstructionCasing")
}

func testBakeCallCheckFlag(t *testing.T, sb integration.Sandbox) {
	dockerfile := []byte(`
FROM scratch
COPy foo /foo
	`)
	dockerfile2 := []byte(`
FROM scratch
COPY foo$BAR /foo
		`)
	bakefile := []byte(`
target "build" {
	dockerfile = "a.Dockerfile"
}

target "another" {
	dockerfile = "b.Dockerfile"
}
`)
	dir := tmpdir(
		t,
		fstest.CreateFile("docker-bake.hcl", bakefile, 0600),
		fstest.CreateFile("a.Dockerfile", dockerfile, 0600),
		fstest.CreateFile("b.Dockerfile", dockerfile2, 0600),
	)

	out, err := bakeCmd(
		sb,
		withDir(dir),
		withArgs("build", "another", "--check"),
	)
	require.Error(t, err, out)

	require.Contains(t, out, "build")
	require.Contains(t, out, "ConsistentInstructionCasing")

	require.Contains(t, out, "another")
	require.Contains(t, out, "UndefinedVar")

	cmd := buildxCmd(
		sb,
		withDir(dir),
		withArgs("bake", "--progress=quiet", "build", "another", "--call", "check,format=json"),
	)
	outB, err := cmd.Output()
	require.Error(t, err, string(outB))

	var res map[string]any
	err = json.Unmarshal(outB, &res)
	require.NoError(t, err, out)

	targets, ok := res["target"].(map[string]any)
	require.True(t, ok)

	build, ok := targets["build"].(map[string]any)
	require.True(t, ok)

	_, ok = build["build"]
	require.True(t, ok)

	check, ok := build["check"].(map[string]any)
	require.True(t, ok)

	warnings, ok := check["warnings"].([]any)
	require.True(t, ok)

	require.Len(t, warnings, 1)

	another, ok := targets["another"].(map[string]any)
	require.True(t, ok)

	_, ok = another["build"]
	require.True(t, ok)

	check, ok = another["check"].(map[string]any)
	require.True(t, ok)

	warnings, ok = check["warnings"].([]any)
	require.True(t, ok)

	require.Len(t, warnings, 1)
}

func testBakeCallMetadata(t *testing.T, sb integration.Sandbox) {
	dockerfile := []byte(`
frOM busybox as base
cOpy Dockerfile .
from scratch
COPy --from=base \
  /Dockerfile \
  /
	`)
	bakefile := []byte(`
target "default" {}
`)
	dir := tmpdir(
		t,
		fstest.CreateFile("docker-bake.hcl", bakefile, 0600),
		fstest.CreateFile("Dockerfile", dockerfile, 0600),
	)

	cmd := buildxCmd(
		sb,
		withDir(dir),
		withArgs("bake", "--call", "check,format=json", "--metadata-file", filepath.Join(dir, "md.json")),
	)
	stdout := bytes.Buffer{}
	stderr := bytes.Buffer{}
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	require.Error(t, cmd.Run(), stdout.String(), stderr.String())

	var res map[string]any
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &res), stdout.String())
	targets, ok := res["target"].(map[string]any)
	require.True(t, ok)
	def, ok := targets["default"].(map[string]any)
	require.True(t, ok)
	_, ok = def["build"]
	require.True(t, ok)
	check, ok := def["check"].(map[string]any)
	require.True(t, ok)
	warnings, ok := check["warnings"].([]any)
	require.True(t, ok)
	require.Len(t, warnings, 3)

	dt, err := os.ReadFile(filepath.Join(dir, "md.json"))
	require.NoError(t, err)

	type mdT struct {
		Default struct {
			BuildRef   string           `json:"buildx.build.ref"`
			ResultJSON lint.LintResults `json:"result.json"`
		} `json:"default"`
	}
	var md mdT
	require.NoError(t, json.Unmarshal(dt, &md), dt)
	require.Empty(t, md.Default.BuildRef)
	require.Len(t, md.Default.ResultJSON.Warnings, 3)
}

func testBakeCheckCallOutput(t *testing.T, sb integration.Sandbox) {
	t.Run("check for warning count msg in check without warnings", func(t *testing.T) {
		dockerfile := []byte(`
FROM busybox
COPY Dockerfile .
		`)
		bakefile := []byte(`
	target "default" {}
	`)
		dir := tmpdir(
			t,
			fstest.CreateFile("docker-bake.hcl", bakefile, 0600),
			fstest.CreateFile("Dockerfile", dockerfile, 0600),
		)

		cmd := buildxCmd(
			sb,
			withDir(dir),
			withArgs("bake", "--call", "check"),
		)
		stdout := bytes.Buffer{}
		stderr := bytes.Buffer{}
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		require.NoError(t, cmd.Run(), stdout.String(), stderr.String())
		require.Contains(t, stdout.String(), "Check complete, no warnings found.")
	})
	t.Run("check for warning count msg in check with single warning", func(t *testing.T) {
		dockerfile := []byte(`
FROM busybox
copy Dockerfile .
		`)
		bakefile := []byte(`
	target "default" {}
	`)
		dir := tmpdir(
			t,
			fstest.CreateFile("docker-bake.hcl", bakefile, 0600),
			fstest.CreateFile("Dockerfile", dockerfile, 0600),
		)

		cmd := buildxCmd(
			sb,
			withDir(dir),
			withArgs("bake", "--call", "check"),
		)
		stdout := bytes.Buffer{}
		stderr := bytes.Buffer{}
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		require.Error(t, cmd.Run(), stdout.String(), stderr.String())
		require.Contains(t, stdout.String(), "Check complete, 1 warning has been found!")
	})
	t.Run("check for warning count msg in check with multiple warnings", func(t *testing.T) {
		dockerfile := []byte(`
FROM busybox
copy Dockerfile .

FROM busybox as base
COPY Dockerfile .
		`)
		bakefile := []byte(`
	target "default" {}
	`)
		dir := tmpdir(
			t,
			fstest.CreateFile("docker-bake.hcl", bakefile, 0600),
			fstest.CreateFile("Dockerfile", dockerfile, 0600),
		)

		cmd := buildxCmd(
			sb,
			withDir(dir),
			withArgs("bake", "--call", "check"),
		)
		stdout := bytes.Buffer{}
		stderr := bytes.Buffer{}
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		require.Error(t, cmd.Run(), stdout.String(), stderr.String())
		require.Contains(t, stdout.String(), "Check complete, 2 warnings have been found!")
	})
	t.Run("check for warnings with multiple build targets", func(t *testing.T) {
		dockerfile1 := []byte(`
FROM busybox
copy Dockerfile .
		`)
		dockerfile2 := []byte(`
FROM busybox
copy Dockerfile .

FROM busybox as base
COPY Dockerfile .
		`)
		bakefile := []byte(`
target "first" {
	dockerfile = "Dockerfile.first"
}
target "second" {
	dockerfile = "Dockerfile.second"
}
	`)
		dir := tmpdir(
			t,
			fstest.CreateFile("docker-bake.hcl", bakefile, 0600),
			fstest.CreateFile("Dockerfile.first", dockerfile1, 0600),
			fstest.CreateFile("Dockerfile.second", dockerfile2, 0600),
		)

		cmd := buildxCmd(
			sb,
			withDir(dir),
			withArgs("bake", "--call", "check", "first", "second"),
		)
		stdout := bytes.Buffer{}
		stderr := bytes.Buffer{}
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		require.Error(t, cmd.Run(), stdout.String(), stderr.String())
		require.Contains(t, stdout.String(), "Check complete, 1 warning has been found!")
		require.Contains(t, stdout.String(), "Check complete, 2 warnings have been found!")
	})
	t.Run("check for Dockerfile path printed with context when displaying rule check warnings with multiple build targets", func(t *testing.T) {
		dockerfile := []byte(`
FROM busybox
copy Dockerfile .
		`)
		bakefile := []byte(`
target "first" {
	dockerfile = "Dockerfile"
}
target "second" {
	dockerfile = "subdir/Dockerfile"
}
target "third" {
	dockerfile = "subdir/subsubdir/Dockerfile"
}
	`)
		dir := tmpdir(
			t,
			fstest.CreateDir("subdir", 0700),
			fstest.CreateDir("subdir/subsubdir", 0700),
			fstest.CreateFile("Dockerfile", dockerfile, 0600),
			fstest.CreateFile("subdir/Dockerfile", dockerfile, 0600),
			fstest.CreateFile("subdir/subsubdir/Dockerfile", dockerfile, 0600),
			fstest.CreateFile("docker-bake.hcl", bakefile, 0600),
		)

		dockerfilePathFirst := "Dockerfile"
		dockerfilePathSecond := filepath.Join("subdir", "Dockerfile")
		dockerfilePathThird := filepath.Join("subdir", "subsubdir", "Dockerfile")

		cmd := buildxCmd(
			sb,
			withDir(dir),
			withArgs("bake", "--call", "check", "first", "second", "third"),
		)
		stdout := bytes.Buffer{}
		stderr := bytes.Buffer{}
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		require.Error(t, cmd.Run(), stdout.String(), stderr.String())
		require.Contains(t, stdout.String(), dockerfilePathFirst+":3")
		require.Contains(t, stdout.String(), dockerfilePathSecond+":3")
		require.Contains(t, stdout.String(), dockerfilePathThird+":3")
	})
}

func writeTempPrivateKey(fp string) error {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return err
	}
	privateKeyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(privateKey),
	})
	return os.WriteFile(fp, privateKeyPEM, 0600)
}
