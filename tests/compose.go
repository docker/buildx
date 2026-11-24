package tests

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
	testComposeBuildDependencies,
	testComposeBuildSecrets,
	testComposeBuildEscaped,
	testComposeBuildExternalDockerfile,
	testComposeBuildNetworkHost,
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

	desc, provider, err := contentutil.ProviderFromRef(sb.Context(), target)
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

		desc, provider, err := contentutil.ProviderFromRef(sb.Context(), target)
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

func testComposeBuildDependencies(t *testing.T, sb integration.Sandbox) {
	if !isDockerWorker(sb) && !isDockerContainerWorker(sb) {
		t.Skip("only testing with docker and docker-container worker")
	}

	target := "buildx:dependencies-" + identity.NewID()
	composefile := fmt.Appendf(nil, `
services:
  base:
    build:
      context: .
      dockerfile_inline: |
        FROM busybox:latest
        COPY marker /marker
  app:
    image: %s
    build:
      context: .
      additional_contexts:
        base: service:base
      dockerfile_inline: |
        FROM base
        RUN test "$(cat /marker)" = from-base
  unrelated:
    build:
      context: .
      dockerfile_inline: |
        FROM busybox:latest
        RUN exit 1
`, target)
	dir := tmpdir(t,
		fstest.CreateFile("compose.yml", composefile, 0600),
		fstest.CreateFile("marker", []byte("from-base"), 0600),
	)
	t.Cleanup(func() {
		cmd := composeCmd(sb, withDir(dir), withArgs("down", "--rmi", "all"))
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, string(out))
	})

	cmd := composeCmd(sb, withDir(dir), withArgs("build", "app"))
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	cmd = dockerCmd(sb, withArgs("run", "--rm", target, "cat", "/marker"))
	out, err = cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	require.Equal(t, "from-base", string(out))
}

func testComposeBuildSecrets(t *testing.T, sb integration.Sandbox) {
	if !isDockerWorker(sb) && !isDockerContainerWorker(sb) {
		t.Skip("only testing with docker and docker-container worker")
	}

	target := "buildx:secrets-" + identity.NewID()
	composefile := fmt.Appendf(nil, `
services:
  app:
    image: %s
    build:
      context: ./context
      secrets:
        - file-secret
        - env-secret
secrets:
  file-secret:
    file: ../secret.txt
  env-secret:
    environment: COMPOSE_TEST_SECRET
`, target)
	dir := tmpdir(t,
		fstest.CreateDir("project/context", 0700),
		fstest.CreateFile("project/compose.yml", composefile, 0600),
		fstest.CreateFile("secret.txt", []byte("file-value"), 0600),
		fstest.CreateFile("project/context/Dockerfile", []byte(`
FROM busybox:latest
RUN --mount=type=secret,id=file-secret,required=true \
    --mount=type=secret,id=env-secret,required=true \
    test "$(cat /run/secrets/file-secret)" = file-value && \
    test "$(cat /run/secrets/env-secret)" = env-value
`), 0600),
	)
	t.Cleanup(func() {
		cmd := dockerCmd(sb, withArgs("image", "rm", "-f", target))
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, string(out))
	})

	cmd := composeCmd(sb, withDir(t.TempDir()), withEnv("COMPOSE_TEST_SECRET=env-value"),
		withArgs("-f", filepath.Join(dir, "project", "compose.yml"), "build"))
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
}

func testComposeBuildEscaped(t *testing.T, sb integration.Sandbox) {
	if !isDockerWorker(sb) && !isDockerContainerWorker(sb) {
		t.Skip("only testing with docker and docker-container worker")
	}

	target := "buildx:escaped-" + identity.NewID()
	composefile := fmt.Appendf(nil, `
services:
  app:
    image: %s
    build:
      context: .
      args:
        VALUE: $${COMPOSE_TEST_LITERAL}
      dockerfile_inline: |
        FROM busybox:latest
        ARG VALUE
        RUN printf '%%s' "$$VALUE" > /arg
        RUN printf '%%s' '$${COMPOSE_TEST_LITERAL}' > /inline
`, target)
	dir := tmpdir(t, fstest.CreateFile("compose.yml", composefile, 0600))
	t.Cleanup(func() {
		cmd := dockerCmd(sb, withArgs("image", "rm", "-f", target))
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, string(out))
	})

	cmd := composeCmd(sb, withDir(dir), withEnv("COMPOSE_TEST_LITERAL=must-not-expand"), withArgs("build"))
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	cmd = dockerCmd(sb, withArgs("run", "--rm", target, "cat", "/arg", "/inline"))
	out, err = cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	require.Equal(t, strings.Repeat("${COMPOSE_TEST_LITERAL}", 2), string(out))
}

func testComposeBuildExternalDockerfile(t *testing.T, sb integration.Sandbox) {
	if !isDockerWorker(sb) && !isDockerContainerWorker(sb) {
		t.Skip("only testing with docker and docker-container worker")
	}

	for _, absolute := range []bool{false, true} {
		t.Run(fmt.Sprintf("absolute=%t", absolute), func(t *testing.T) {
			target := "buildx:external-dockerfile-" + identity.NewID()
			dir := tmpdir(t,
				fstest.CreateDir("project/context", 0700),
				fstest.CreateDir("docker", 0700),
				fstest.CreateFile("project/context/keep", []byte("keep"), 0600),
				fstest.CreateFile("project/context/ignore", []byte("ignore"), 0600),
				fstest.CreateFile("docker/Dockerfile.prod.dockerignore", []byte("ignore\n"), 0600),
				fstest.CreateFile("docker/Dockerfile.prod", []byte(`
FROM busybox:latest
COPY . /input/
RUN test "$(cat /input/keep)" = keep && test ! -e /input/ignore
`), 0600),
			)
			dockerfile := "../../docker/Dockerfile.prod"
			if absolute {
				dockerfile = filepath.Join(dir, "docker", "Dockerfile.prod")
			}
			composefile := fmt.Appendf(nil, `
services:
  app:
    image: %s
    build:
      context: ./context
      dockerfile: %q
`, target, dockerfile)
			require.NoError(t, os.WriteFile(filepath.Join(dir, "project", "compose.yml"), composefile, 0600))
			t.Cleanup(func() {
				cmd := dockerCmd(sb, withArgs("image", "rm", "-f", target))
				out, err := cmd.CombinedOutput()
				require.NoError(t, err, string(out))
			})

			cmd := composeCmd(sb, withDir(t.TempDir()), withEnv("BUILDX_BAKE_ENTITLEMENTS_FS=1"),
				withArgs("-f", filepath.Join(dir, "project", "compose.yml"), "build"))
			out, err := cmd.CombinedOutput()
			require.NoError(t, err, string(out))
		})
	}
}

func testComposeBuildNetworkHost(t *testing.T, sb integration.Sandbox) {
	if !isDockerWorker(sb) && !isDockerContainerWorker(sb) {
		t.Skip("only testing with docker and docker-container worker")
	}

	for _, tc := range []struct {
		name   string
		config string
		run    string
	}{
		{
			name:   "network",
			config: "network: host",
			run:    "RUN true",
		},
		{
			name:   "entitlement",
			config: "entitlements: [network.host]",
			run:    "RUN --network=host true",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			target := "buildx:network-host-" + identity.NewID()
			composefile := fmt.Appendf(nil, `
services:
  app:
    image: %s
    build:
      context: .
      %s
`, target, tc.config)
			dir := tmpdir(t,
				fstest.CreateFile("compose.yml", composefile, 0600),
				fstest.CreateFile("Dockerfile", []byte("FROM busybox:latest\n"+tc.run+"\n"), 0600),
			)
			t.Cleanup(func() {
				cmd := dockerCmd(sb, withArgs("image", "rm", "-f", target))
				out, err := cmd.CombinedOutput()
				require.NoError(t, err, string(out))
			})

			cmd := composeCmd(sb, withDir(dir), withArgs("build", "--no-cache"))
			out, err := cmd.CombinedOutput()
			require.NoError(t, err, string(out))
		})
	}
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
