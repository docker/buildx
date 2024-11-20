package tests

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/containerd/continuity/fs/fstest"
	"github.com/containerd/platforms"
	"github.com/creack/pty"
	"github.com/docker/buildx/localstate"
	"github.com/docker/buildx/util/confutil"
	"github.com/docker/buildx/util/gitutil"
	"github.com/moby/buildkit/client"
	"github.com/moby/buildkit/frontend/subrequests/lint"
	"github.com/moby/buildkit/frontend/subrequests/outline"
	"github.com/moby/buildkit/frontend/subrequests/targets"
	"github.com/moby/buildkit/identity"
	provenancetypes "github.com/moby/buildkit/solver/llbsolver/provenance/types"
	"github.com/moby/buildkit/util/appdefaults"
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
	testBuildAlias,
	testBuildStdin,
	testBuildRemote,
	testBuildLocalState,
	testBuildLocalStateStdin,
	testBuildLocalStateRemote,
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
	testBuildMultiPlatform,
	testDockerHostGateway,
	testBuildNetworkModeBridge,
	testBuildShmSize,
	testBuildUlimit,
	testBuildMetadataProvenance,
	testBuildMetadataWarnings,
	testBuildMultiExporters,
	testBuildLoadPush,
	testBuildSecret,
	testBuildDefaultLoad,
	testBuildCall,
	testCheckCallOutput,
}

func testBuild(t *testing.T, sb integration.Sandbox) {
	dir := createTestProject(t)
	out, err := buildCmd(sb, withArgs(dir))
	require.NoError(t, err, string(out))
}

func testBuildAlias(t *testing.T, sb integration.Sandbox) {
	dir := createTestProject(t)
	cmd := buildxCmd(sb, withDir(dir), withArgs("b", dir))
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
}

func testBuildStdin(t *testing.T, sb integration.Sandbox) {
	dockerfile := []byte(`
FROM busybox:latest AS base
COPY foo /etc/foo
RUN cp /etc/foo /etc/bar

FROM scratch
COPY --from=base /etc/bar /bar
`)
	dir := tmpdir(
		t,
		fstest.CreateFile("foo", []byte("foo"), 0600),
	)

	cmd := buildxCmd(sb, withDir(dir), withArgs("build", "--progress=quiet", "-f-", dir))
	cmd.Stdin = bytes.NewReader(dockerfile)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
}

func testBuildRemote(t *testing.T, sb integration.Sandbox) {
	dockerfile := []byte(`
FROM busybox:latest
COPY foo /foo
`)
	dir := tmpdir(
		t,
		fstest.CreateFile("Dockerfile", dockerfile, 0600),
		fstest.CreateFile("foo", []byte("foo"), 0600),
	)
	dirDest := t.TempDir()

	git, err := gitutil.New(gitutil.WithWorkingDir(dir))
	require.NoError(t, err)

	gitutil.GitInit(git, t)
	gitutil.GitAdd(git, t, "Dockerfile", "foo")
	gitutil.GitCommit(git, t, "initial commit")
	addr := gitutil.GitServeHTTP(git, t)

	out, err := buildCmd(sb, withDir(dir), withArgs("--output=type=local,dest="+dirDest, addr))
	require.NoError(t, err, out)
	require.FileExists(t, filepath.Join(dirDest, "foo"))
}

func testBuildLocalState(t *testing.T, sb integration.Sandbox) {
	dockerfile := []byte(`
FROM busybox:latest AS base
COPY foo /etc/foo
RUN cp /etc/foo /etc/bar

FROM scratch
COPY --from=base /etc/bar /bar
`)
	dir := tmpdir(
		t,
		fstest.CreateFile("build.Dockerfile", dockerfile, 0600),
		fstest.CreateFile("foo", []byte("foo"), 0600),
	)

	out, err := buildCmd(sb, withDir(dir), withArgs(
		"-f", "build.Dockerfile",
		"--metadata-file", filepath.Join(dir, "md.json"),
		".",
	))
	require.NoError(t, err, out)

	dt, err := os.ReadFile(filepath.Join(dir, "md.json"))
	require.NoError(t, err)

	type mdT struct {
		BuildRef string `json:"buildx.build.ref"`
	}
	var md mdT
	err = json.Unmarshal(dt, &md)
	require.NoError(t, err)

	ls, err := localstate.New(confutil.NewConfig(nil, confutil.WithDir(buildxConfig(sb))))
	require.NoError(t, err)

	refParts := strings.Split(md.BuildRef, "/")
	require.Len(t, refParts, 3)

	ref, err := ls.ReadRef(refParts[0], refParts[1], refParts[2])
	require.NoError(t, err)
	require.NotNil(t, ref)
	require.DirExists(t, ref.LocalPath)
	require.FileExists(t, ref.DockerfilePath)
}

func testBuildLocalStateStdin(t *testing.T, sb integration.Sandbox) {
	dockerfile := []byte(`
FROM busybox:latest AS base
COPY foo /etc/foo
RUN cp /etc/foo /etc/bar

FROM scratch
COPY --from=base /etc/bar /bar
`)
	dir := tmpdir(
		t,
		fstest.CreateFile("foo", []byte("foo"), 0600),
	)

	cmd := buildxCmd(sb, withDir(dir), withArgs("build", "--progress=quiet", "--metadata-file", filepath.Join(dir, "md.json"), "-f-", dir))
	cmd.Stdin = bytes.NewReader(dockerfile)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))

	dt, err := os.ReadFile(filepath.Join(dir, "md.json"))
	require.NoError(t, err)

	type mdT struct {
		BuildRef string `json:"buildx.build.ref"`
	}
	var md mdT
	err = json.Unmarshal(dt, &md)
	require.NoError(t, err)

	ls, err := localstate.New(confutil.NewConfig(nil, confutil.WithDir(buildxConfig(sb))))
	require.NoError(t, err)

	refParts := strings.Split(md.BuildRef, "/")
	require.Len(t, refParts, 3)

	ref, err := ls.ReadRef(refParts[0], refParts[1], refParts[2])
	require.NoError(t, err)
	require.NotNil(t, ref)
	require.DirExists(t, ref.LocalPath)
	require.Equal(t, "-", ref.DockerfilePath)
}

func testBuildLocalStateRemote(t *testing.T, sb integration.Sandbox) {
	dockerfile := []byte(`
FROM busybox:latest
COPY foo /foo
`)
	dir := tmpdir(
		t,
		fstest.CreateFile("build.Dockerfile", dockerfile, 0600),
		fstest.CreateFile("foo", []byte("foo"), 0600),
	)
	dirDest := t.TempDir()

	git, err := gitutil.New(gitutil.WithWorkingDir(dir))
	require.NoError(t, err)

	gitutil.GitInit(git, t)
	gitutil.GitAdd(git, t, "build.Dockerfile", "foo")
	gitutil.GitCommit(git, t, "initial commit")
	addr := gitutil.GitServeHTTP(git, t)

	out, err := buildCmd(sb, withDir(dir), withArgs(
		"-f", "build.Dockerfile",
		"--metadata-file", filepath.Join(dirDest, "md.json"),
		"--output", "type=local,dest="+dirDest,
		addr,
	))
	require.NoError(t, err, out)
	require.FileExists(t, filepath.Join(dirDest, "foo"))

	dt, err := os.ReadFile(filepath.Join(dirDest, "md.json"))
	require.NoError(t, err)

	type mdT struct {
		BuildRef string `json:"buildx.build.ref"`
	}
	var md mdT
	err = json.Unmarshal(dt, &md)
	require.NoError(t, err)

	ls, err := localstate.New(confutil.NewConfig(nil, confutil.WithDir(buildxConfig(sb))))
	require.NoError(t, err)

	refParts := strings.Split(md.BuildRef, "/")
	require.Len(t, refParts, 3)

	ref, err := ls.ReadRef(refParts[0], refParts[1], refParts[2])
	require.NoError(t, err)
	require.NotNil(t, ref)
	require.Equal(t, addr, ref.LocalPath)
	require.Equal(t, "build.Dockerfile", ref.DockerfilePath)
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
	if isMobyWorker(sb) {
		require.Error(t, err)
		require.Contains(t, out, "Attestation is not supported")
		return
	} else if !isMobyContainerdSnapWorker(sb) && !matchesBuildKitVersion(t, sb, ">= 0.11.0-0") {
		require.Error(t, err)
		require.Contains(t, out, "Attestations are not supported by the current BuildKit daemon")
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
		t.Skip("only testing with docker workers")
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
	skipNoCompatBuildKit(t, sb, ">= 0.11.0-0", "build details link")
	buildDetailsPattern := regexp.MustCompile(`(?m)^View build details: docker-desktop://dashboard/build/[^/]+/[^/]+/[^/]+\n$`)

	// build simple dockerfile
	dockerfile := []byte(`FROM busybox:latest
RUN echo foo > /bar`)
	dir := tmpdir(t, fstest.CreateFile("Dockerfile", dockerfile, 0600))
	cmd := buildxCmd(sb, withArgs("build", "--output=type=cacheonly", dir))
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	require.False(t, buildDetailsPattern.MatchString(string(out)), "build details link not expected in output, got %q", out)

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
	require.True(t, buildDetailsPattern.MatchString(string(out)), "expected build details link in output, got %q", out)

	// build erroneous dockerfile
	dockerfile = []byte(`FROM busybox:latest
RUN exit 1`)
	dir = tmpdir(t, fstest.CreateFile("Dockerfile", dockerfile, 0600))
	cmd = buildxCmd(sb, withArgs("build", "--output=type=cacheonly", dir))
	out, err = cmd.CombinedOutput()
	require.Error(t, err, string(out))
	require.True(t, buildDetailsPattern.MatchString(string(out)), "expected build details link in output, got %q", out)
}

func testBuildProgress(t *testing.T, sb integration.Sandbox) {
	dir := createTestProject(t)
	sbDriver, _, _ := driverName(sb.Name())
	name := sb.Address()

	// progress=tty
	cmd := buildxCmd(sb, withArgs("build", "--progress=tty", "--output=type=cacheonly", dir))
	f, err := pty.Start(cmd)
	require.NoError(t, err)
	buf := bytes.NewBuffer(nil)
	io.Copy(buf, f)
	ttyOutput := buf.String()
	require.Contains(t, ttyOutput, "[+] Building")
	require.Contains(t, ttyOutput, fmt.Sprintf("%s:%s", sbDriver, name))
	require.Contains(t, ttyOutput, "=> [internal] load build definition from Dockerfile")
	require.Contains(t, ttyOutput, "=> [base 1/3] FROM docker.io/library/busybox:latest")

	// progress=plain
	cmd = buildxCmd(sb, withArgs("build", "--progress=plain", "--output=type=cacheonly", dir))
	plainOutput, err := cmd.CombinedOutput()
	require.NoError(t, err)
	require.Contains(t, string(plainOutput), fmt.Sprintf(`#0 building with "%s" instance using %s driver`, name, sbDriver))
	require.Contains(t, string(plainOutput), "[internal] load build definition from Dockerfile")
	require.Contains(t, string(plainOutput), "[base 1/3] FROM docker.io/library/busybox:latest")
}

func testBuildAnnotations(t *testing.T, sb integration.Sandbox) {
	if isMobyWorker(sb) {
		t.Skip("annotations not supported on docker worker")
	}
	skipNoCompatBuildKit(t, sb, ">= 0.11.0-0", "annotations")

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
		"--annotation", "manifest-descriptor[" + platforms.Format(platforms.DefaultSpec()) + "]:example4=zzz",
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
	require.Equal(t, `ERROR: invalid key-value pair "=TEST_STRING": empty key`, strings.TrimSpace(string(out)))
}

func testBuildLabelNoKey(t *testing.T, sb integration.Sandbox) {
	dir := createTestProject(t)
	cmd := buildxCmd(sb, withArgs("build", "--label", "=TEST_STRING", dir))
	out, err := cmd.CombinedOutput()
	require.Error(t, err, string(out))
	require.Equal(t, `ERROR: invalid key-value pair "=TEST_STRING": empty key`, strings.TrimSpace(string(out)))
}

func testBuildCacheExportNotSupported(t *testing.T, sb integration.Sandbox) {
	if !isMobyWorker(sb) {
		t.Skip("only testing with docker worker")
	}

	dir := createTestProject(t)
	cmd := buildxCmd(sb, withArgs("build", "--cache-to=type=registry", dir))
	out, err := cmd.CombinedOutput()
	require.Error(t, err, string(out))
	require.Contains(t, string(out), "Cache export is not supported")
}

func testBuildOCIExportNotSupported(t *testing.T, sb integration.Sandbox) {
	if !isMobyWorker(sb) {
		t.Skip("only testing with docker worker")
	}

	dir := createTestProject(t)
	cmd := buildxCmd(sb, withArgs("build", fmt.Sprintf("--output=type=oci,dest=%s/result", dir), dir))
	out, err := cmd.CombinedOutput()
	require.Error(t, err, string(out))
	require.Contains(t, string(out), "OCI exporter is not supported")
}

func testBuildMultiPlatform(t *testing.T, sb integration.Sandbox) {
	dockerfile := []byte(`
	FROM --platform=$BUILDPLATFORM busybox:latest AS base
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
	registry, err := sb.NewRegistry()
	if errors.Is(err, integration.ErrRequirements) {
		t.Skip(err.Error())
	}
	require.NoError(t, err)
	target := registry + "/buildx/registry:latest"

	cmd := buildxCmd(sb, withArgs("build", "--platform=linux/amd64,linux/arm64", fmt.Sprintf("--output=type=image,name=%s,push=true", target), dir))
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

func testDockerHostGateway(t *testing.T, sb integration.Sandbox) {
	dockerfile := []byte(`
FROM busybox
RUN ping -c 1 buildx.host-gateway-ip.local
`)
	dir := tmpdir(t, fstest.CreateFile("Dockerfile", dockerfile, 0600))
	cmd := buildxCmd(sb, withArgs("build", "--add-host=buildx.host-gateway-ip.local:host-gateway", "--output=type=cacheonly", dir))
	out, err := cmd.CombinedOutput()
	if !isDockerWorker(sb) {
		require.Error(t, err, string(out))
		require.Contains(t, string(out), "host-gateway is not supported")
	} else {
		require.NoError(t, err, string(out))
	}
}

func testBuildNetworkModeBridge(t *testing.T, sb integration.Sandbox) {
	if !isDockerContainerWorker(sb) {
		t.Skip("only testing with docker-container worker")
	}
	skipNoCompatBuildKit(t, sb, ">= 0.13.0-0", "network bridge")

	var builderName string
	t.Cleanup(func() {
		if builderName == "" {
			return
		}
		out, err := rmCmd(sb, withArgs(builderName))
		require.NoError(t, err, out)
	})

	out, err := createCmd(sb, withArgs(
		"--driver", "docker-container",
		"--buildkitd-flags=--oci-worker-net=bridge --allow-insecure-entitlement=network.host",
	))
	require.NoError(t, err, out)
	builderName = strings.TrimSpace(out)

	dockerfile := []byte(`
FROM busybox AS build
RUN ip a show eth0 | awk '/inet / {split($2, a, "/"); print a[1]}' > /ip-bridge.txt
RUN --network=host ip a show eth0 | awk '/inet / {split($2, a, "/"); print a[1]}' > /ip-host.txt
FROM scratch
COPY --from=build /ip*.txt /`)
	dir := tmpdir(t, fstest.CreateFile("Dockerfile", dockerfile, 0600))

	cmd := buildxCmd(sb, withArgs("build", "--allow=network.host", fmt.Sprintf("--output=type=local,dest=%s", dir), dir))
	cmd.Env = append(cmd.Env, "BUILDX_BUILDER="+builderName)
	outb, err := cmd.CombinedOutput()
	require.NoError(t, err, string(outb))

	dt, err := os.ReadFile(filepath.Join(dir, "ip-bridge.txt"))
	require.NoError(t, err)

	ipBridge := net.ParseIP(strings.TrimSpace(string(dt)))
	require.NotNil(t, ipBridge)

	_, subnet, err := net.ParseCIDR(appdefaults.BridgeSubnet)
	require.NoError(t, err)
	require.True(t, subnet.Contains(ipBridge))

	dt, err = os.ReadFile(filepath.Join(dir, "ip-host.txt"))
	require.NoError(t, err)

	ip := net.ParseIP(strings.TrimSpace(string(dt)))
	require.NotNil(t, ip)

	require.NotEqual(t, ip, ipBridge)
}

func testBuildShmSize(t *testing.T, sb integration.Sandbox) {
	dockerfile := []byte(`
FROM busybox AS build
RUN mount | grep /dev/shm > /shmsize
FROM scratch
COPY --from=build /shmsize /
	`)
	dir := tmpdir(
		t,
		fstest.CreateFile("Dockerfile", dockerfile, 0600),
	)

	cmd := buildxCmd(sb, withArgs("build", "--shm-size=128m", fmt.Sprintf("--output=type=local,dest=%s", dir), dir))
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))

	dt, err := os.ReadFile(filepath.Join(dir, "shmsize"))
	require.NoError(t, err)
	require.Contains(t, string(dt), `size=131072k`)
}

func testBuildUlimit(t *testing.T, sb integration.Sandbox) {
	dockerfile := []byte(`
FROM busybox AS build
RUN ulimit -n > first > /ulimit
FROM scratch
COPY --from=build /ulimit /
	`)
	dir := tmpdir(
		t,
		fstest.CreateFile("Dockerfile", dockerfile, 0600),
	)

	cmd := buildxCmd(sb, withArgs("build", "--ulimit=nofile=1024:1024", fmt.Sprintf("--output=type=local,dest=%s", dir), dir))
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))

	dt, err := os.ReadFile(filepath.Join(dir, "ulimit"))
	require.NoError(t, err)
	require.Contains(t, string(dt), `1024`)
}

func testBuildMetadataProvenance(t *testing.T, sb integration.Sandbox) {
	t.Run("default", func(t *testing.T) {
		buildMetadataProvenance(t, sb, "")
	})
	t.Run("max", func(t *testing.T) {
		buildMetadataProvenance(t, sb, "max")
	})
	t.Run("min", func(t *testing.T) {
		buildMetadataProvenance(t, sb, "min")
	})
	t.Run("disabled", func(t *testing.T) {
		buildMetadataProvenance(t, sb, "disabled")
	})
}

func buildMetadataProvenance(t *testing.T, sb integration.Sandbox, metadataMode string) {
	dir := createTestProject(t)
	dirDest := t.TempDir()

	outFlag := "--output=type=docker"
	if sb.DockerAddress() == "" {
		// there is no Docker atm to load the image
		outFlag += ",dest=" + dirDest + "/image.tar"
	}

	cmd := buildxCmd(
		sb,
		withArgs("build", outFlag, "--metadata-file", filepath.Join(dirDest, "md.json"), dir),
		withEnv("BUILDX_METADATA_PROVENANCE="+metadataMode),
	)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))

	dt, err := os.ReadFile(filepath.Join(dirDest, "md.json"))
	require.NoError(t, err)

	type mdT struct {
		BuildRef        string                 `json:"buildx.build.ref"`
		BuildProvenance map[string]interface{} `json:"buildx.build.provenance"`
	}
	var md mdT
	err = json.Unmarshal(dt, &md)
	require.NoError(t, err)

	require.NotEmpty(t, md.BuildRef)
	if metadataMode == "disabled" {
		require.Empty(t, md.BuildProvenance)
		return
	}
	require.NotEmpty(t, md.BuildProvenance)

	dtprv, err := json.Marshal(md.BuildProvenance)
	require.NoError(t, err)

	var prv provenancetypes.ProvenancePredicate
	require.NoError(t, json.Unmarshal(dtprv, &prv))
	require.Equal(t, provenancetypes.BuildKitBuildType, prv.BuildType)
}

func testBuildMetadataWarnings(t *testing.T, sb integration.Sandbox) {
	t.Run("default", func(t *testing.T) {
		buildMetadataWarnings(t, sb, "")
	})
	t.Run("true", func(t *testing.T) {
		buildMetadataWarnings(t, sb, "true")
	})
	t.Run("false", func(t *testing.T) {
		buildMetadataWarnings(t, sb, "false")
	})
}

func buildMetadataWarnings(t *testing.T, sb integration.Sandbox, mode string) {
	dockerfile := []byte(`
frOM busybox as base
cOpy Dockerfile .
from scratch
COPy --from=base \
  /Dockerfile \
  /
	`)
	dir := tmpdir(
		t,
		fstest.CreateFile("Dockerfile", dockerfile, 0600),
	)

	cmd := buildxCmd(
		sb,
		withArgs("build", "--metadata-file", filepath.Join(dir, "md.json"), dir),
		withEnv("BUILDX_METADATA_WARNINGS="+mode),
	)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))

	dt, err := os.ReadFile(filepath.Join(dir, "md.json"))
	require.NoError(t, err)

	type mdT struct {
		BuildRef      string                 `json:"buildx.build.ref"`
		BuildWarnings []client.VertexWarning `json:"buildx.build.warnings"`
	}
	var md mdT
	err = json.Unmarshal(dt, &md)
	require.NoError(t, err, string(dt))

	require.NotEmpty(t, md.BuildRef, string(dt))
	if mode == "" || mode == "false" {
		require.Empty(t, md.BuildWarnings, string(dt))
		return
	}

	skipNoCompatBuildKit(t, sb, ">= 0.14.0-0", "lint")
	require.Len(t, md.BuildWarnings, 3, string(dt))
}

func testBuildMultiExporters(t *testing.T, sb integration.Sandbox) {
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

	dir := createTestProject(t)

	outputs := []string{
		"--output", fmt.Sprintf("type=image,name=%s,push=true", targetReg),
		"--output", fmt.Sprintf("type=docker,name=%s", targetStore),
		"--output", fmt.Sprintf("type=oci,dest=%s/result", dir),
	}
	cmd := buildxCmd(sb, withArgs("build"), withArgs(outputs...), withArgs(dir))
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

func testBuildLoadPush(t *testing.T, sb integration.Sandbox) {
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

	dir := createTestProject(t)

	cmd := buildxCmd(sb, withArgs(
		"build", "--push", "--load",
		fmt.Sprintf("-t=%s", target),
		dir,
	))
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

func testBuildSecret(t *testing.T, sb integration.Sandbox) {
	token := "abcd1234"
	dockerfile := []byte(`
FROM busybox AS build
RUN --mount=type=secret,id=token cat /run/secrets/token | tee /token
FROM scratch
COPY --from=build /token /
	`)
	dir := tmpdir(
		t,
		fstest.CreateFile("Dockerfile", dockerfile, 0600),
		fstest.CreateFile("tokenfile", []byte(token), 0600),
	)

	t.Run("env", func(t *testing.T) {
		t.Cleanup(func() {
			_ = os.Remove(filepath.Join(dir, "token"))
		})

		cmd := buildxCmd(sb, withEnv("TOKEN="+token), withArgs("build", "--secret=id=token,env=TOKEN", fmt.Sprintf("--output=type=local,dest=%s", dir), dir))
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, string(out))

		dt, err := os.ReadFile(filepath.Join(dir, "token"))
		require.NoError(t, err)
		require.Equal(t, token, string(dt))
	})

	t.Run("file", func(t *testing.T) {
		t.Cleanup(func() {
			_ = os.Remove(filepath.Join(dir, "token"))
		})

		cmd := buildxCmd(sb, withArgs("build", "--secret=id=token,src="+path.Join(dir, "tokenfile"), fmt.Sprintf("--output=type=local,dest=%s", dir), dir))
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, string(out))

		dt, err := os.ReadFile(filepath.Join(dir, "token"))
		require.NoError(t, err)
		require.Equal(t, token, string(dt))
	})
}

func testBuildDefaultLoad(t *testing.T, sb integration.Sandbox) {
	if !isDockerWorker(sb) {
		t.Skip("only testing with docker workers")
	}

	tag := "buildx/build:" + identity.NewID()

	var builderName string
	t.Cleanup(func() {
		if builderName == "" {
			return
		}

		cmd := dockerCmd(sb, withArgs("image", "rm", tag))
		cmd.Stderr = os.Stderr
		require.NoError(t, cmd.Run())

		out, err := rmCmd(sb, withArgs(builderName))
		require.NoError(t, err, out)
	})

	out, err := createCmd(sb, withArgs(
		"--driver", "docker-container",
		"--driver-opt", "default-load=true",
	))
	require.NoError(t, err, out)
	builderName = strings.TrimSpace(out)

	dir := createTestProject(t)

	cmd := buildxCmd(sb, withArgs(
		"build",
		fmt.Sprintf("-t=%s", tag),
		dir,
	))
	cmd.Env = append(cmd.Env, "BUILDX_BUILDER="+builderName)
	outb, err := cmd.CombinedOutput()
	require.NoError(t, err, string(outb))

	cmd = dockerCmd(sb, withArgs("image", "inspect", tag))
	cmd.Stderr = os.Stderr
	require.NoError(t, cmd.Run())
}

func testBuildCall(t *testing.T, sb integration.Sandbox) {
	t.Run("check", func(t *testing.T) {
		dockerfile := []byte(`
frOM busybox as base
cOpy Dockerfile .
from scratch
COPy --from=base \
  /Dockerfile \
  /
	`)
		dir := tmpdir(
			t,
			fstest.CreateFile("Dockerfile", dockerfile, 0600),
		)

		cmd := buildxCmd(sb, withArgs("build", "--call=check,format=json", dir))
		stdout := bytes.Buffer{}
		stderr := bytes.Buffer{}
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		require.Error(t, cmd.Run(), stdout.String(), stderr.String())

		var res lint.LintResults
		require.NoError(t, json.Unmarshal(stdout.Bytes(), &res))
		require.Equal(t, 3, len(res.Warnings))
	})

	t.Run("outline", func(t *testing.T) {
		dockerfile := []byte(`
FROM busybox AS first
RUN --mount=type=secret,target=/etc/passwd,required=true --mount=type=ssh true

FROM alpine AS second
RUN --mount=type=secret,id=unused --mount=type=ssh,id=ssh2 true

FROM scratch AS third
ARG BAR
RUN --mount=type=secret,id=second${BAR} true

FROM third AS target
COPY --from=first /foo /
RUN --mount=type=ssh,id=ssh3,required true

FROM second
	`)
		dir := tmpdir(
			t,
			fstest.CreateFile("Dockerfile", dockerfile, 0600),
		)

		cmd := buildxCmd(sb, withArgs("build", "--build-arg=BAR=678", "--target=target", "--call=outline,format=json", dir))
		stdout := bytes.Buffer{}
		stderr := bytes.Buffer{}
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		require.NoError(t, cmd.Run(), stdout.String(), stderr.String())

		var res outline.Outline
		require.NoError(t, json.Unmarshal(stdout.Bytes(), &res))
		assert.Equal(t, "target", res.Name)

		require.Equal(t, 1, len(res.Args))
		assert.Equal(t, "BAR", res.Args[0].Name)
		assert.Equal(t, "678", res.Args[0].Value)

		require.Equal(t, 2, len(res.Secrets))
		assert.Equal(t, "passwd", res.Secrets[0].Name)
		assert.Equal(t, true, res.Secrets[0].Required)
		assert.Equal(t, "second678", res.Secrets[1].Name)
		assert.Equal(t, false, res.Secrets[1].Required)

		require.Equal(t, 2, len(res.SSH))
		assert.Equal(t, "default", res.SSH[0].Name)
		assert.Equal(t, false, res.SSH[0].Required)
		assert.Equal(t, "ssh3", res.SSH[1].Name)
		assert.Equal(t, true, res.SSH[1].Required)

		require.Equal(t, 1, len(res.Sources))
	})

	t.Run("targets", func(t *testing.T) {
		dockerfile := []byte(`
# build defines stage for compiling the binary
FROM alpine AS build
RUN true

FROM busybox as second
RUN false

FROM alpine
RUN false

# binary returns the compiled binary
FROM second AS binary
	`)
		dir := tmpdir(
			t,
			fstest.CreateFile("Dockerfile", dockerfile, 0600),
		)

		cmd := buildxCmd(sb, withArgs("build", "--call=targets,format=json", dir))
		stdout := bytes.Buffer{}
		stderr := bytes.Buffer{}
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		require.NoError(t, cmd.Run(), stdout.String(), stderr.String())

		var res targets.List
		require.NoError(t, json.Unmarshal(stdout.Bytes(), &res))

		require.Equal(t, 4, len(res.Targets))
		assert.Equal(t, "build", res.Targets[0].Name)
		assert.Equal(t, "defines stage for compiling the binary", res.Targets[0].Description)
		assert.Equal(t, "alpine", res.Targets[0].Base)
		assert.Equal(t, "second", res.Targets[1].Name)
		assert.Empty(t, res.Targets[1].Description)
		assert.Equal(t, "busybox", res.Targets[1].Base)
		assert.Empty(t, res.Targets[2].Name)
		assert.Empty(t, res.Targets[2].Description)
		assert.Equal(t, "alpine", res.Targets[2].Base)
		assert.Equal(t, "binary", res.Targets[3].Name)
		assert.Equal(t, "returns the compiled binary", res.Targets[3].Description)
		assert.Equal(t, "second", res.Targets[3].Base)
		assert.Equal(t, true, res.Targets[3].Default)

		require.Equal(t, 1, len(res.Sources))
	})

	t.Run("check metadata", func(t *testing.T) {
		dockerfile := []byte(`
frOM busybox as base
cOpy Dockerfile .
from scratch
COPy --from=base \
  /Dockerfile \
  /
	`)
		dir := tmpdir(
			t,
			fstest.CreateFile("Dockerfile", dockerfile, 0600),
		)

		cmd := buildxCmd(sb, withArgs("build", "--call=check,format=json", "--metadata-file", filepath.Join(dir, "md.json"), dir))
		stdout := bytes.Buffer{}
		stderr := bytes.Buffer{}
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		require.Error(t, cmd.Run(), stdout.String(), stderr.String())

		var res lint.LintResults
		require.NoError(t, json.Unmarshal(stdout.Bytes(), &res), stdout.String())
		require.Len(t, res.Warnings, 3)

		dt, err := os.ReadFile(filepath.Join(dir, "md.json"))
		require.NoError(t, err)

		type mdT struct {
			BuildRef   string           `json:"buildx.build.ref"`
			ResultJSON lint.LintResults `json:"result.json"`
		}
		var md mdT
		require.NoError(t, json.Unmarshal(dt, &md), dt)
		require.Empty(t, md.BuildRef)
		require.Len(t, md.ResultJSON.Warnings, 3)
	})
}

func testCheckCallOutput(t *testing.T, sb integration.Sandbox) {
	t.Run("check for warning count msg in check without warnings", func(t *testing.T) {
		dockerfile := []byte(`
FROM busybox AS base
COPY Dockerfile .
	`)
		dir := tmpdir(
			t,
			fstest.CreateFile("Dockerfile", dockerfile, 0600),
		)

		cmd := buildxCmd(sb, withArgs("build", "--call=check", dir))
		stdout := bytes.Buffer{}
		stderr := bytes.Buffer{}
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		require.NoError(t, cmd.Run(), stdout.String(), stderr.String())
		require.Contains(t, stdout.String(), "Check complete, no warnings found.")
	})

	t.Run("check for warning count msg in check with single warning", func(t *testing.T) {
		dockerfile := []byte(`
FROM busybox as base
COPY Dockerfile .
	`)
		dir := tmpdir(
			t,
			fstest.CreateFile("Dockerfile", dockerfile, 0600),
		)

		cmd := buildxCmd(sb, withArgs("build", "--call=check", dir))
		stdout := bytes.Buffer{}
		stderr := bytes.Buffer{}
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		require.Error(t, cmd.Run(), stdout.String(), stderr.String())
		require.Contains(t, stdout.String(), "Check complete, 1 warning has been found!")
	})

	t.Run("check for warning count msg in check with multiple warnings", func(t *testing.T) {
		dockerfile := []byte(`
frOM busybox as base
cOpy Dockerfile .
	`)
		dir := tmpdir(
			t,
			fstest.CreateFile("Dockerfile", dockerfile, 0600),
		)

		cmd := buildxCmd(sb, withArgs("build", "--call=check", dir))
		stdout := bytes.Buffer{}
		stderr := bytes.Buffer{}
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		require.Error(t, cmd.Run(), stdout.String(), stderr.String())
		require.Contains(t, stdout.String(), "Check complete, 2 warnings have been found!")
	})

	t.Run("check for Dockerfile path printed with context when displaying rule check warnings", func(t *testing.T) {
		dockerfile := []byte(`
frOM busybox as base
cOpy Dockerfile .
	`)
		dir := tmpdir(
			t,
			fstest.CreateDir("subdir", 0700),
			fstest.CreateFile("subdir/Dockerfile", dockerfile, 0600),
		)
		dockerfilePath := filepath.Join(dir, "subdir", "Dockerfile")

		cmd := buildxCmd(sb, withArgs("build", "--call=check", "-f", dockerfilePath, dir))
		stdout := bytes.Buffer{}
		stderr := bytes.Buffer{}
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		require.Error(t, cmd.Run(), stdout.String(), stderr.String())
		require.Contains(t, stdout.String(), "Check complete, 2 warnings have been found!")
		require.Contains(t, stdout.String(), dockerfilePath+":2")
		require.Contains(t, stdout.String(), dockerfilePath+":3")
	})
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
