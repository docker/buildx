package tests

import (
	"fmt"
	"os"
	"testing"

	"github.com/containerd/continuity/fs/fstest"
	"github.com/moby/buildkit/identity"
	"github.com/moby/buildkit/util/contentutil"
	"github.com/moby/buildkit/util/testutil"
	"github.com/moby/buildkit/util/testutil/integration"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"
)

var composeTests = []func(t *testing.T, sb integration.Sandbox){
	testComposeBuildLocalStore,
	testComposeBuildRegistry,
	testComposeBuildMultiPlatform,
	testComposeBuildCheck,
}

func testComposeBuildLocalStore(t *testing.T, sb integration.Sandbox) {
	if !isDockerWorker(sb) && !isDockerContainerWorker(sb) {
		t.Skip("only testing with docker and docker-container worker")
	}

	target := "buildx:local-" + identity.NewID()
	dir := composeTestProject(target, t)

	t.Cleanup(func() {
		cmd := dockerCmd(sb, withArgs("image", "rm", target))
		cmd.Stderr = os.Stderr
		require.NoError(t, cmd.Run())
	})

	cmd := composeCmd(sb, withDir(dir), withArgs("build"))
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))

	cmd = dockerCmd(sb, withArgs("image", "inspect", target))
	cmd.Stderr = os.Stderr
	require.NoError(t, cmd.Run())
}

func testComposeBuildRegistry(t *testing.T, sb integration.Sandbox) {
	registry, err := sb.NewRegistry()
	if errors.Is(err, integration.ErrRequirements) {
		t.Skip(err.Error())
	}
	require.NoError(t, err)

	target := registry + "/buildx/registry:latest"
	dir := composeTestProject(target, t)

	cmd := composeCmd(sb, withDir(dir), withArgs("build", "--push"))
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))

	desc, provider, err := contentutil.ProviderFromRef(target)
	require.NoError(t, err)
	_, err = testutil.ReadImages(sb.Context(), provider, desc)
	require.NoError(t, err)
}

func testComposeBuildMultiPlatform(t *testing.T, sb integration.Sandbox) {
	registry, err := sb.NewRegistry()
	if errors.Is(err, integration.ErrRequirements) {
		t.Skip(err.Error())
	}
	require.NoError(t, err)

	target := registry + "/buildx/registry:latest"

	dockerfile := []byte(`
FROM busybox:latest
COPY foo /etc/foo
`)
	composefile := fmt.Appendf([]byte{}, `
services:
  bar:
    build:
      context: .
      platforms:
        - linux/amd64
        - linux/arm64
    image: %s
`, target)

	dir := tmpdir(
		t,
		fstest.CreateFile("compose.yml", composefile, 0600),
		fstest.CreateFile("Dockerfile", dockerfile, 0600),
		fstest.CreateFile("foo", []byte("foo"), 0600),
	)

	cmd := composeCmd(sb, withDir(dir), withArgs("build", "--push"))
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

func testComposeBuildCheck(t *testing.T, sb integration.Sandbox) {
	dockerfile := []byte(`
frOM busybox as base
cOpy Dockerfile .
from scratch
COPy --from=base \
  /Dockerfile \
  /
	`)

	composefile := []byte(`
services:
  bar:
    build:
      context: .
`)

	dir := tmpdir(
		t,
		fstest.CreateFile("compose.yml", composefile, 0600),
		fstest.CreateFile("Dockerfile", dockerfile, 0600),
	)

	cmd := composeCmd(sb, withDir(dir), withArgs("build", "--check"))
	out, err := cmd.CombinedOutput()
	require.Error(t, err, string(out))
	require.Contains(t, string(out), "Check complete, 3 warnings have been found!")
}

func composeTestProject(imageName string, t *testing.T) string {
	dockerfile := []byte(`
FROM busybox:latest AS base
COPY foo /etc/foo
RUN cp /etc/foo /etc/bar

FROM scratch
COPY --from=base /etc/bar /bar
`)

	composefile := fmt.Appendf([]byte{}, `
services:
  bar:
    build:
      context: .
    image: %s
`, imageName)

	return tmpdir(
		t,
		fstest.CreateFile("compose.yml", composefile, 0600),
		fstest.CreateFile("Dockerfile", dockerfile, 0600),
		fstest.CreateFile("foo", []byte("foo"), 0600),
	)
}
