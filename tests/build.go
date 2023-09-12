package tests

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/containerd/containerd/platforms"
	"github.com/containerd/continuity/fs/fstest"
	"github.com/creack/pty"
	"github.com/moby/buildkit/util/contentutil"
	"github.com/moby/buildkit/util/testutil"
	"github.com/moby/buildkit/util/testutil/integration"
	"github.com/opencontainers/go-digest"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
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
	testBuildRegistryExportAttestations,
	testBuildTarExport,
	testBuildMobyFromLocalImage,
	testBuildDetailsLink,
	testBuildProgress,
	testBuildAnnotations,
	testBuildBuildArgNoKey,
	testBuildLabelNoKey,
	testBuildCacheExportNotSupported,
	testBuildOCIExportNotSupported,
	testBuildMultiPlatformNotSupported,
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

func testBuildRegistryExportAttestations(t *testing.T, sb integration.Sandbox) {
	dir := createTestProject(t)

	registry, err := sb.NewRegistry()
	if errors.Is(err, integration.ErrRequirements) {
		t.Skip(err.Error())
	}
	require.NoError(t, err)
	target := registry + "/buildx/registry:latest"

	out, err := buildCmd(sb, withArgs(fmt.Sprintf("--output=type=image,name=%s,push=true", target), "--provenance=true", dir))
	if sb.Name() == "docker" {
		require.Error(t, err)
		require.Contains(t, out, "attestations are not supported")
		return
	}
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

	att := imgs.FindAttestation(pk)
	require.NotNil(t, att)
	require.Len(t, att.Layers, 1)
}

func testImageIDOutput(t *testing.T, sb integration.Sandbox) {
	dockerfile := []byte(`FROM busybox:latest`)

	dir := tmpdir(t,
		fstest.CreateFile("Dockerfile", dockerfile, 0600),
	)
	targetDir := t.TempDir()

	outFlag := "--output=type=docker"

	if sb.DockerAddress() == "" {
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
	cmd = dockerCmd(sb, withArgs("tag", "busybox:latest", "busybox:1.35"))
	cmd.Stderr = os.Stderr
	require.NoError(t, cmd.Run())

	// build image and check that it uses the local tag
	// (note: the version check should match the version of busybox in pins.go)
	dockerfile = []byte(`
FROM busybox:1.35
RUN busybox | head -1 | grep v1.36.1
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

func testBuildProgress(t *testing.T, sb integration.Sandbox) {
	dir := createTestProject(t)
	driver, _, _ := strings.Cut(sb.Name(), "+")
	name := sb.Address()

	// progress=tty
	cmd := buildxCmd(sb, withArgs("build", "--progress=tty", "--output=type=cacheonly", dir))
	f, err := pty.Start(cmd)
	require.NoError(t, err)
	buf := bytes.NewBuffer(nil)
	io.Copy(buf, f)
	ttyOutput := buf.String()
	require.Contains(t, ttyOutput, "[+] Building")
	require.Contains(t, ttyOutput, fmt.Sprintf("%s:%s", driver, name))
	require.Contains(t, ttyOutput, "=> [internal] load build definition from Dockerfile")
	require.Contains(t, ttyOutput, "=> [base 1/3] FROM docker.io/library/busybox:latest")

	// progress=plain
	cmd = buildxCmd(sb, withArgs("build", "--progress=plain", "--output=type=cacheonly", dir))
	plainOutput, err := cmd.CombinedOutput()
	require.NoError(t, err)
	require.Contains(t, string(plainOutput), fmt.Sprintf(`#0 building with "%s" instance using %s driver`, name, driver))
	require.Contains(t, string(plainOutput), "[internal] load build definition from Dockerfile")
	require.Contains(t, string(plainOutput), "[base 1/3] FROM docker.io/library/busybox:latest")
}

func testBuildAnnotations(t *testing.T, sb integration.Sandbox) {
	if sb.Name() == "docker" {
		t.Skip("annotations not supported on docker worker")
	}

	dir := createTestProject(t)

	registry, err := sb.NewRegistry()
	if errors.Is(err, integration.ErrRequirements) {
		t.Skip(err.Error())
	}
	require.NoError(t, err)
	target := registry + "/buildx/registry:latest"

	annotations := []string{
		"--annotation", "example1=www",
		"--annotation", "index:example2=xxx",
		"--annotation", "manifest:example3=yyy",
		"--annotation", "manifest-descriptor[" + platforms.DefaultString() + "]:example4=zzz",
	}
	out, err := buildCmd(sb, withArgs(annotations...), withArgs(fmt.Sprintf("--output=type=image,name=%s,push=true", target), dir))
	require.NoError(t, err, string(out))

	desc, provider, err := contentutil.ProviderFromRef(target)
	require.NoError(t, err)
	imgs, err := testutil.ReadImages(sb.Context(), provider, desc)
	require.NoError(t, err)

	pk := platforms.Format(platforms.Normalize(platforms.DefaultSpec()))
	img := imgs.Find(pk)
	require.NotNil(t, img)

	require.NotNil(t, imgs.Index)
	assert.Equal(t, "xxx", imgs.Index.Annotations["example2"])

	require.NotNil(t, img.Manifest)
	assert.Equal(t, "www", img.Manifest.Annotations["example1"])
	assert.Equal(t, "yyy", img.Manifest.Annotations["example3"])

	require.NotNil(t, img.Desc)
	assert.Equal(t, "zzz", img.Desc.Annotations["example4"])
}

func testBuildBuildArgNoKey(t *testing.T, sb integration.Sandbox) {
	dir := createTestProject(t)
	cmd := buildxCmd(sb, withArgs("build", "--build-arg", "=TEST_STRING", dir))
	out, err := cmd.CombinedOutput()
	require.Error(t, err, string(out))
	require.Equal(t, strings.TrimSpace(string(out)), `ERROR: invalid key-value pair "=TEST_STRING": empty key`)
}

func testBuildLabelNoKey(t *testing.T, sb integration.Sandbox) {
	dir := createTestProject(t)
	cmd := buildxCmd(sb, withArgs("build", "--label", "=TEST_STRING", dir))
	out, err := cmd.CombinedOutput()
	require.Error(t, err, string(out))
	require.Equal(t, strings.TrimSpace(string(out)), `ERROR: invalid key-value pair "=TEST_STRING": empty key`)
}

func testBuildCacheExportNotSupported(t *testing.T, sb integration.Sandbox) {
	if sb.Name() != "docker" {
		t.Skip("skipping test for non-docker workers")
	}

	dir := createTestProject(t)
	cmd := buildxCmd(sb, withArgs("build", "--cache-to=type=registry", dir))
	out, err := cmd.CombinedOutput()
	require.Error(t, err, string(out))
	require.Contains(t, string(out), "Cache export is not supported")
}

func testBuildOCIExportNotSupported(t *testing.T, sb integration.Sandbox) {
	if sb.Name() != "docker" {
		t.Skip("skipping test for non-docker workers")
	}

	dir := createTestProject(t)
	cmd := buildxCmd(sb, withArgs("build", fmt.Sprintf("--output=type=oci,dest=%s/result", dir), dir))
	out, err := cmd.CombinedOutput()
	require.Error(t, err, string(out))
	require.Contains(t, string(out), "OCI exporter is not supported")
}

func testBuildMultiPlatformNotSupported(t *testing.T, sb integration.Sandbox) {
	if sb.Name() != "docker" {
		t.Skip("skipping test for non-docker workers")
	}

	dir := createTestProject(t)
	cmd := buildxCmd(sb, withArgs("build", "--platform=linux/amd64,linux/arm64", dir))
	out, err := cmd.CombinedOutput()
	require.Error(t, err, string(out))
	require.Contains(t, string(out), "Multi-platform build is not supported")
}
