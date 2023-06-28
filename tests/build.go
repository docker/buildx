package tests

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/containerd/containerd/platforms"
	"github.com/containerd/continuity/fs/fstest"
	"github.com/moby/buildkit/util/contentutil"
	"github.com/moby/buildkit/util/testutil"
	"github.com/moby/buildkit/util/testutil/integration"
	"github.com/opencontainers/go-digest"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"
)

func buildCmd(sb integration.Sandbox, opts ...cmdOpt) (string, error) {
	opts = append([]cmdOpt{withArgs("build", "--progress=quiet")}, opts...)
	cmd := buildxCmd(sb, opts...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

var buildTests = []func(t *testing.T, sb integration.Sandbox){
	testBuild,
	testImageIDOutput,
	testBuildLocalExport,
	testBuildRegistryExport,
	testBuildTarExport,
	testBuildMobyFromLocalImage,
	testBuildDetailsLink,
}

func testBuild(t *testing.T, sb integration.Sandbox) {
	dir := createTestProject(t)
	out, err := buildCmd(sb, withArgs(dir))
	require.NoError(t, err, string(out))
}

func testBuildLocalExport(t *testing.T, sb integration.Sandbox) {
	dir := createTestProject(t)
	out, err := buildCmd(sb, withArgs(fmt.Sprintf("--output=type=local,dest=%s/result", dir), dir))
	require.NoError(t, err, string(out))

	dt, err := os.ReadFile(dir + "/result/bar")
	require.NoError(t, err)
	require.Equal(t, "foo", string(dt))
}

func testBuildTarExport(t *testing.T, sb integration.Sandbox) {
	dir := createTestProject(t)
	out, err := buildCmd(sb, withArgs(fmt.Sprintf("--output=type=tar,dest=%s/result.tar", dir), dir))
	require.NoError(t, err, string(out))

	dt, err := os.ReadFile(fmt.Sprintf("%s/result.tar", dir))
	require.NoError(t, err)
	m, err := testutil.ReadTarToMap(dt, false)
	require.NoError(t, err)

	require.Contains(t, m, "bar")
	require.Equal(t, "foo", string(m["bar"].Data))
}

func testBuildRegistryExport(t *testing.T, sb integration.Sandbox) {
	dir := createTestProject(t)

	registry, err := sb.NewRegistry()
	if errors.Is(err, integration.ErrRequirements) {
		t.Skip(err.Error())
	}
	require.NoError(t, err)
	target := registry + "/buildx/registry:latest"

	out, err := buildCmd(sb, withArgs(fmt.Sprintf("--output=type=image,name=%s,push=true", target), dir))
	require.NoError(t, err, string(out))

	desc, provider, err := contentutil.ProviderFromRef(target)
	require.NoError(t, err)
	imgs, err := testutil.ReadImages(sb.Context(), provider, desc)
	require.NoError(t, err)

	pk := platforms.Format(platforms.Normalize(platforms.DefaultSpec()))
	img := imgs.Find(pk)
	require.NotNil(t, img)
	require.Len(t, img.Layers, 1)
	require.Equal(t, img.Layers[0]["bar"].Data, []byte("foo"))
}

func testImageIDOutput(t *testing.T, sb integration.Sandbox) {
	dockerfile := []byte(`FROM busybox:latest`)

	dir := tmpdir(t,
		fstest.CreateFile("Dockerfile", dockerfile, 0600),
	)
	targetDir := t.TempDir()

	outFlag := "--output=type=docker"

	if sb.Name() == "remote" {
		// there is no Docker atm to load the image
		outFlag += ",dest=" + targetDir + "/image.tar"
	}

	cmd := buildxCmd(
		sb,
		withArgs("build", "-q", "--provenance", "false", outFlag, "--iidfile", filepath.Join(targetDir, "iid.txt"), "--metadata-file", filepath.Join(targetDir, "md.json"), dir),
	)
	stdout := bytes.NewBuffer(nil)
	cmd.Stdout = stdout
	cmd.Stderr = os.Stderr
	err := cmd.Run()
	require.NoError(t, err)

	dt, err := os.ReadFile(filepath.Join(targetDir, "iid.txt"))
	require.NoError(t, err)

	imageID := string(dt)
	require.NotEmpty(t, imageID)

	dgst, err := digest.Parse(string(dt))
	require.NoError(t, err)

	require.Equal(t, dgst.String(), strings.TrimSpace(stdout.String()))

	dt, err = os.ReadFile(filepath.Join(targetDir, "md.json"))
	require.NoError(t, err)

	type mdT struct {
		ConfigDigest string `json:"containerimage.config.digest"`
	}
	var md mdT
	err = json.Unmarshal(dt, &md)
	require.NoError(t, err)

	require.NotEmpty(t, md.ConfigDigest)
	require.Equal(t, dgst, digest.Digest(md.ConfigDigest))
}

func testBuildMobyFromLocalImage(t *testing.T, sb integration.Sandbox) {
	if !isDockerWorker(sb) {
		t.Skip("skipping test for non-docker workers")
	}

	// pull image
	cmd := dockerCmd(sb, withArgs("pull", "-q", "busybox:latest"))
	stdout := bytes.NewBuffer(nil)
	cmd.Stdout = stdout
	cmd.Stderr = os.Stderr
	require.NoError(t, cmd.Run())
	require.Equal(t, "docker.io/library/busybox:latest", strings.TrimSpace(stdout.String()))

	// create local tag
	cmd = dockerCmd(sb, withArgs("tag", "busybox:latest", "buildx-test:busybox"))
	cmd.Stderr = os.Stderr
	require.NoError(t, cmd.Run())

	// build image
	dockerfile := []byte(`FROM buildx-test:busybox`)
	dir := tmpdir(t, fstest.CreateFile("Dockerfile", dockerfile, 0600))
	cmd = buildxCmd(
		sb,
		withArgs("build", "-q", "--output=type=cacheonly", dir),
	)
	cmd.Stderr = os.Stderr
	require.NoError(t, cmd.Run())

	// create local tag matching a remote one
	cmd = dockerCmd(sb, withArgs("tag", "busybox:latest", "busybox:1.36"))
	cmd.Stderr = os.Stderr
	require.NoError(t, cmd.Run())

	// build image and check that it uses the local tag
	dockerfile = []byte(`
FROM busybox:1.36
RUN busybox | head -1 | grep v1.35.0
`)
	dir = tmpdir(t, fstest.CreateFile("Dockerfile", dockerfile, 0600))
	cmd = buildxCmd(
		sb,
		withArgs("build", "-q", "--output=type=cacheonly", dir),
	)
	cmd.Stderr = os.Stderr
	require.NoError(t, cmd.Run())
}

func testBuildDetailsLink(t *testing.T, sb integration.Sandbox) {
	buildDetailsPattern := regexp.MustCompile(`(?m)^View build details: docker-desktop://dashboard/build/[^/]+/[^/]+/[^/]+\n$`)

	// build simple dockerfile
	dockerfile := []byte(`FROM busybox:latest
RUN echo foo > /bar`)
	dir := tmpdir(t, fstest.CreateFile("Dockerfile", dockerfile, 0600))
	cmd := buildxCmd(sb, withArgs("build", "--output=type=cacheonly", dir))
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	require.False(t, buildDetailsPattern.MatchString(string(out)), fmt.Sprintf("build details link not expected in output, got %q", out))

	// create desktop-build .lastaccess file
	home, err := os.UserHomeDir() // TODO: sandbox should create a temp home dir and expose it through its interface
	require.NoError(t, err)
	dbDir := path.Join(home, ".docker", "desktop-build")
	require.NoError(t, os.MkdirAll(dbDir, 0755))
	dblaFile, err := os.Create(path.Join(dbDir, ".lastaccess"))
	require.NoError(t, err)
	defer func() {
		dblaFile.Close()
		if err := os.Remove(dblaFile.Name()); err != nil {
			t.Fatal(err)
		}
	}()

	// build again
	cmd = buildxCmd(sb, withArgs("build", "--output=type=cacheonly", dir))
	out, err = cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	require.True(t, buildDetailsPattern.MatchString(string(out)), fmt.Sprintf("expected build details link in output, got %q", out))

	// build erroneous dockerfile
	dockerfile = []byte(`FROM busybox:latest
RUN exit 1`)
	dir = tmpdir(t, fstest.CreateFile("Dockerfile", dockerfile, 0600))
	cmd = buildxCmd(sb, withArgs("build", "--output=type=cacheonly", dir))
	out, err = cmd.CombinedOutput()
	require.Error(t, err, string(out))
	require.True(t, buildDetailsPattern.MatchString(string(out)), fmt.Sprintf("expected build details link in output, got %q", out))
}

func createTestProject(t *testing.T) string {
	dockerfile := []byte(`
FROM busybox:latest AS base
COPY foo /etc/foo
RUN cp /etc/foo /etc/bar

FROM scratch
COPY --from=base /etc/bar /bar
`)
	dir := tmpdir(
		t,
		fstest.CreateFile("Dockerfile", dockerfile, 0600),
		fstest.CreateFile("foo", []byte("foo"), 0600),
	)
	return dir
}
