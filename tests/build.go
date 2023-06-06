package tests

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/containerd/containerd/platforms"
	"github.com/containerd/continuity/fs/fstest"
	"github.com/moby/buildkit/util/contentutil"
	"github.com/moby/buildkit/util/testutil"
	"github.com/moby/buildkit/util/testutil/integration"
	"github.com/opencontainers/go-digest"
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
		withArgs("build", "-q", outFlag, "--iidfile", filepath.Join(targetDir, "iid.txt"), "--metadata-file", filepath.Join(targetDir, "md.json"), dir),
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
