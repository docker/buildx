package tests

import (
	"errors"
	"fmt"
	"os"
	"testing"

	"github.com/containerd/containerd/platforms"
	"github.com/containerd/continuity/fs/fstest"
	"github.com/moby/buildkit/util/contentutil"
	"github.com/moby/buildkit/util/testutil"
	"github.com/moby/buildkit/util/testutil/integration"
	"github.com/stretchr/testify/require"
)

func buildCmd(sb integration.Sandbox, args ...string) (string, error) {
	args = append([]string{"build", "--progress=quiet"}, args...)
	cmd := buildxCmd(sb, args...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

var buildTests = []func(t *testing.T, sb integration.Sandbox){
	testBuild,
	testBuildLocalExport,
	testBuildRegistryExport,
	testBuildTarExport,
}

func testBuild(t *testing.T, sb integration.Sandbox) {
	dir := createTestProject(t)
	out, err := buildCmd(sb, dir)
	require.NoError(t, err, string(out))
}

func testBuildLocalExport(t *testing.T, sb integration.Sandbox) {
	dir := createTestProject(t)
	out, err := buildCmd(sb, fmt.Sprintf("--output=type=local,dest=%s/result", dir), dir)
	require.NoError(t, err, string(out))

	dt, err := os.ReadFile(dir + "/result/bar")
	require.NoError(t, err)
	require.Equal(t, "foo", string(dt))
}

func testBuildTarExport(t *testing.T, sb integration.Sandbox) {
	dir := createTestProject(t)
	out, err := buildCmd(sb, fmt.Sprintf("--output=type=tar,dest=%s/result.tar", dir), dir)
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

	out, err := buildCmd(sb, fmt.Sprintf("--output=type=image,name=%s,push=true", target), dir)
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

func createTestProject(t *testing.T) string {
	dockerfile := []byte(`
FROM busybox:latest AS base
COPY foo /etc/foo
RUN cp /etc/foo /etc/bar

FROM scratch
COPY --from=base /etc/bar /bar
`)
	dir, err := tmpdir(
		t,
		fstest.CreateFile("Dockerfile", dockerfile, 0600),
		fstest.CreateFile("foo", []byte("foo"), 0600),
	)
	require.NoError(t, err)
	return dir
}
