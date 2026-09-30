package tests

import (
	"archive/tar"
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/containerd/platforms"
	"github.com/moby/buildkit/identity"
	"github.com/moby/buildkit/util/testutil/integration"
	ocispecs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
)

// replayTests exercises the buildx replay subcommands.
//
// All tests require a registry sandbox for the build-to-replay round-trip
// because replay resolves a subject through the registry resolver. Tests
// that need a writable registry skip when `sb.RegistryAddress()` is empty.
var replayTests = []func(t *testing.T, sb integration.Sandbox){
	testReplayBuildRoundTrip,
	testReplayRejectsChangedHTTPContext,
	testReplaySnapshotExportAndRejectsOfflineReplay,
	testReplayVerifyDigest,
	testReplayRejectsLocalContext,
	testReplayRejectsIncompleteProvenance,
	testReplaySecretRoundTrip,
	testReplayMultiPlatformRoundTrip,
	testReplayDefaultPlatformUsesWorkerDefault,
}

// replayRegistry skips workers that cannot produce or replay provenance and
// returns a registry for the round-trip.
func replayRegistry(t *testing.T, sb integration.Sandbox) string {
	t.Helper()
	if isMobyWorker(sb) {
		t.Skip("attestations are not supported by the docker worker")
	}
	skipNoCompatBuildKit(t, sb, ">= 0.27.0-0", "replay requires session source policy")
	registry, err := sb.NewRegistry()
	if err != nil {
		t.Skipf("skipping: registry not available: %v", err)
	}
	return registry
}

func replayTestTag(t *testing.T) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '.', r == '-':
			return r
		default:
			return '-'
		}
	}, t.Name())
}

// replayTestDockerfile is a minimal Dockerfile that COPYs from a named
// context so the build's provenance records no local filesystem context —
// the default buildx build always records a local context which replay
// correctly refuses.
const replayTestDockerfile = `# syntax=docker/dockerfile:1
FROM scratch
COPY --from=ctx /etc/hosts /hosts
`

func buildReplayContext(t *testing.T, dockerfile string) (string, string) {
	t.Helper()
	dt := replayContextArchive(t, dockerfile)
	contextPath := filepath.Join(t.TempDir(), "context.tar")
	require.NoError(t, os.WriteFile(contextPath, dt, 0o600))

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/x-tar")
		_, _ = w.Write(dt)
	}))
	t.Cleanup(server.Close)
	return server.URL + "/context.tar", contextPath
}

func replayContextArchive(t *testing.T, dockerfile string) []byte {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	require.NoError(t, tw.WriteHeader(&tar.Header{Name: "Dockerfile", Mode: 0o600, Size: int64(len(dockerfile))}))
	_, err := tw.Write([]byte(dockerfile))
	require.NoError(t, err)
	require.NoError(t, tw.Close())
	return buf.Bytes()
}

// buildReplayableImage does a `buildx build` against a registry using a
// remotely served context, so the resulting image has valid SLSA v1
// provenance without a local context. It returns the registry reference and
// both forms of the remote context needed by snapshot tests.
func buildReplayableImage(t *testing.T, sb integration.Sandbox, extra ...string) (string, string, string) {
	t.Helper()
	registry := replayRegistry(t, sb)
	ref := registry + "/buildx-replay:" + replayTestTag(t)
	contextRef, contextPath := buildReplayContext(t, replayTestDockerfile)

	args := []string{
		"--output=type=registry,name=" + ref,
		"--build-context=ctx=docker-image://alpine:latest",
		"--attest=type=provenance,mode=max",
		contextRef,
	}
	args = append(args, extra...)
	out, err := buildCmd(sb, withArgs(args...))
	require.NoError(t, err, out)
	return ref, contextRef, contextPath
}

func testReplayBuildRoundTrip(t *testing.T, sb integration.Sandbox) {
	ref, _, _ := buildReplayableImage(t, sb)

	dest := filepath.Join(t.TempDir(), "replay-out")
	cmd := buildxCmd(sb, withArgs(
		"replay", "build",
		"docker-image://"+ref,
		"--output=type=oci,dest="+filepath.Join(dest, "replay.oci.tar"),
	))
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	require.FileExists(t, filepath.Join(dest, "replay.oci.tar"))
}

func testReplayRejectsChangedHTTPContext(t *testing.T, sb integration.Sandbox) {
	registry := replayRegistry(t, sb)
	ref := registry + "/buildx-replay:" + replayTestTag(t)

	var mu sync.RWMutex
	contextBytes := replayContextArchive(t, replayTestDockerfile)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.RLock()
		dt := bytes.Clone(contextBytes)
		mu.RUnlock()
		w.Header().Set("Content-Type", "application/x-tar")
		_, _ = w.Write(dt)
	}))
	t.Cleanup(server.Close)
	contextRef := server.URL + "/context.tar"

	out, err := buildCmd(sb, withArgs(
		"--output=type=registry,name="+ref,
		"--build-context=ctx=docker-image://alpine:latest",
		"--attest=type=provenance,mode=max",
		contextRef,
	))
	require.NoError(t, err, out)
	prune := buildxCmd(sb, withArgs("prune", "--all", "--force"))
	pruneOut, err := prune.CombinedOutput()
	require.NoError(t, err, string(pruneOut))

	changedContext := replayContextArchive(t, replayTestDockerfile+"# changed after provenance was recorded\n")
	mu.Lock()
	contextBytes = changedContext
	mu.Unlock()

	cmd := buildxCmd(sb, withArgs(
		"replay", "build",
		"docker-image://"+ref,
		"--output=type=oci,dest="+filepath.Join(t.TempDir(), "replay.oci.tar"),
	))
	outBytes, err := cmd.CombinedOutput()
	require.Error(t, err, string(outBytes))
	require.Contains(t, string(outBytes), "digest mismatch")
}

func testReplaySnapshotExportAndRejectsOfflineReplay(t *testing.T, sb integration.Sandbox) {
	ref, contextRef, contextPath := buildReplayableImage(t, sb)

	dest := filepath.Join(t.TempDir(), "snap")
	cmd := buildxCmd(sb, withArgs(
		"replay", "snapshot",
		"docker-image://"+ref,
		"--materials=provenance",
		"--materials="+contextRef+"="+contextPath,
		"--output=type=local,dest="+dest,
	))
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	require.FileExists(t, filepath.Join(dest, "oci-layout"))

	// Snapshot-backed input injection is not implemented yet. Reject the
	// explicit store rather than silently replaying from the network.
	outDir := filepath.Join(t.TempDir(), "replay-from-snapshot")
	cmd = buildxCmd(sb, withArgs(
		"replay", "build",
		"docker-image://"+ref,
		"--materials=oci-layout://"+dest,
		"--output=type=oci,dest="+filepath.Join(outDir, "replay.oci.tar"),
	))
	out, err = cmd.CombinedOutput()
	require.Error(t, err, string(out))
	require.Contains(t, string(out), "not implemented")
}

func testReplayVerifyDigest(t *testing.T, sb integration.Sandbox) {
	// The replay runs on the builder that produced the image, so cached
	// steps reproduce the original digest.
	ref, _, _ := buildReplayableImage(t, sb)

	cmd := buildxCmd(sb, withArgs(
		"replay", "verify",
		"docker-image://"+ref,
		"--compare=digest",
		"--progress=rawjson",
	))
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	require.Contains(t, string(out), `"vertexes"`, "--progress must reach the verify build printer")
}

func testReplayRejectsLocalContext(t *testing.T, sb integration.Sandbox) {
	// A default `buildx build` records a local filesystem context. Replay
	// must refuse this case.
	registry := replayRegistry(t, sb)
	ref := registry + "/buildx-replay-local:" + replayTestTag(t)

	dir := createTestProject(t)
	out, err := buildCmd(sb, withArgs(
		"--output=type=registry,name="+ref,
		"--attest=type=provenance,mode=max",
		dir,
	))
	require.NoError(t, err, out)

	cmd := buildxCmd(sb, withArgs(
		"replay", "build",
		"docker-image://"+ref,
	))
	bout, err := cmd.CombinedOutput()
	require.Error(t, err, string(bout))
	require.Contains(t, string(bout), "image was built from local files")
}

// testReplayRejectsIncompleteProvenance checks that images without provenance
// or with mode=min provenance fail in dry-run with an explanation.
func testReplayRejectsIncompleteProvenance(t *testing.T, sb integration.Sandbox) {
	registry := replayRegistry(t, sb)
	contextRef, _ := buildReplayContext(t, replayTestDockerfile)

	for _, tc := range []struct {
		name  string
		attrs []string
		err   string
	}{
		{name: "no-provenance", attrs: []string{"--provenance=false"}, err: "no SLSA provenance attestation found"},
		{name: "min-mode", attrs: []string{"--attest=type=provenance,mode=min"}, err: "provenance was recorded with mode=min"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ref := registry + "/buildx-replay-" + tc.name + ":" + replayTestTag(t)
			args := append([]string{
				"--output=type=registry,name=" + ref,
				"--build-context=ctx=docker-image://alpine:latest",
				contextRef,
			}, tc.attrs...)
			out, err := buildCmd(sb, withArgs(args...))
			require.NoError(t, err, out)

			cmd := buildxCmd(sb, withArgs("replay", "build", "docker-image://"+ref, "--dry-run"))
			bout, err := cmd.CombinedOutput()
			require.Error(t, err, string(bout))
			require.Contains(t, string(bout), tc.err)
			require.Contains(t, string(bout), "--provenance=mode=max")
		})
	}
}

func testReplaySecretRoundTrip(t *testing.T, sb integration.Sandbox) {
	// Build with a declared secret so the provenance records a required
	// secret ID. Replay without --secret must fail; with --secret passes.
	registry := replayRegistry(t, sb)
	ref := registry + "/buildx-replay-secret:" + replayTestTag(t)

	secretFile := filepath.Join(t.TempDir(), "secret.txt")
	require.NoError(t, os.WriteFile(secretFile, []byte("hunter2"), 0o600))

	dockerfile := `# syntax=docker/dockerfile:1
FROM alpine:latest
RUN --mount=type=secret,id=api,required=true cp /run/secrets/api /secret
`
	contextRef, _ := buildReplayContext(t, dockerfile)

	out, err := buildCmd(sb, withArgs(
		"--output=type=registry,name="+ref,
		"--secret=id=api,src="+secretFile,
		"--attest=type=provenance,mode=max",
		contextRef,
	))
	require.NoError(t, err, out)

	// Without --secret: fail with missing-secret exit code.
	cmd := buildxCmd(sb, withArgs(
		"replay", "build",
		"docker-image://"+ref,
		"--output=type=oci,dest="+filepath.Join(t.TempDir(), "out.oci.tar"),
	))
	bout, err := cmd.CombinedOutput()
	require.Error(t, err, string(bout))
	require.Contains(t, string(bout), "missing required secrets", string(bout))

	// With --secret: succeed.
	cmd = buildxCmd(sb, withArgs(
		"replay", "build",
		"docker-image://"+ref,
		"--secret=id=api,src="+secretFile,
		"--output=type=oci,dest="+filepath.Join(t.TempDir(), "out.oci.tar"),
	))
	bout, err = cmd.CombinedOutput()
	require.NoError(t, err, string(bout))
}

func testReplayMultiPlatformRoundTrip(t *testing.T, sb integration.Sandbox) {
	if !isRemoteMultiNodeWorker(sb) {
		t.Skip("only testing with remote multi-node worker")
	}
	registry := replayRegistry(t, sb)
	ref := registry + "/buildx-replay-mp:" + replayTestTag(t)

	contextRef, _ := buildReplayContext(t, replayTestDockerfile)

	out, err := buildCmd(sb, withArgs(
		"--output=type=registry,name="+ref,
		"--build-context=ctx=docker-image://alpine:latest",
		"--attest=type=provenance,mode=max",
		"--platform=linux/amd64,linux/arm64",
		contextRef,
	))
	require.NoError(t, err, out)

	// Each platform of the index can be selected for replay.
	for _, platform := range []string{"linux/amd64", "linux/arm64"} {
		cmd := buildxCmd(sb, withArgs(
			"replay", "build",
			"docker-image://"+ref,
			"--platform="+platform,
			"--dry-run",
			"--format=json",
		))
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		require.NoError(t, cmd.Run(), stderr.String())
		var plan struct {
			Subjects []struct {
				Descriptor ocispecs.Descriptor `json:"descriptor"`
			} `json:"subjects"`
		}
		require.NoError(t, json.Unmarshal(stdout.Bytes(), &plan), "dry-run must emit JSON plan")
		require.Len(t, plan.Subjects, 1)
		require.NotNil(t, plan.Subjects[0].Descriptor.Platform)
		require.Equal(t, platform, platforms.Format(*plan.Subjects[0].Descriptor.Platform))
	}

	// Replaying every platform at once is rejected up front, including in
	// dry-run.
	cmd := buildxCmd(sb, withArgs(
		"replay", "build",
		"docker-image://"+ref,
		"--platform=all",
		"--dry-run",
	))
	bout, err := cmd.CombinedOutput()
	require.Error(t, err, string(bout))
	require.Contains(t, string(bout), "select a single platform with --platform")
}

// testReplayDefaultPlatformUsesWorkerDefault checks that replay without
// --platform selects the subject for the platform BuildKit builds by default,
// not the first platform configured on the builder node.
func testReplayDefaultPlatformUsesWorkerDefault(t *testing.T, sb integration.Sandbox) {
	if !isRemoteWorker(sb) {
		t.Skip("only testing with remote workers")
	}
	native := platforms.Format(platforms.Normalize(platforms.DefaultSpec()))
	var other string
	switch native {
	case "linux/amd64":
		other = "linux/arm64"
	case "linux/arm64":
		other = "linux/amd64"
	default:
		t.Skipf("unsupported host platform %s", native)
	}
	registry := replayRegistry(t, sb)
	ref := registry + "/buildx-replay-default-platform:" + replayTestTag(t)

	contextRef, _ := buildReplayContext(t, replayTestDockerfile)
	out, err := buildCmd(sb, withArgs(
		"--output=type=registry,name="+ref,
		"--build-context=ctx=docker-image://alpine:latest",
		"--attest=type=provenance,mode=max",
		"--platform=linux/amd64,linux/arm64",
		contextRef,
	))
	require.NoError(t, err, out)

	out, err = inspectCmd(sb)
	require.NoError(t, err, out)
	var endpoint string
	for line := range strings.Lines(out) {
		if v, ok := strings.CutPrefix(line, "Endpoint:"); ok {
			endpoint = strings.TrimSpace(v)
			break
		}
	}
	require.NotEmpty(t, endpoint, out)

	// Configure the same worker with the non-native platform listed first.
	// BuildKit still builds for the worker's native platform by default.
	name := "replay-default-platform-" + identity.NewID()
	out, err = createCmd(sb, withArgs(
		"--name="+name,
		"--driver=remote",
		"--platform="+other+","+native,
		endpoint,
	))
	require.NoError(t, err, out)
	t.Cleanup(func() {
		out, err := rmCmd(sb, withArgs(name))
		require.NoError(t, err, out)
	})
	// Only the first node decides the default platform. A secondary node
	// that cannot be booted must not be touched.
	out, err = createCmd(sb, withArgs(
		"--append",
		"--name="+name,
		"--driver=remote",
		"tcp://127.0.0.1:1",
	))
	require.NoError(t, err, out)

	cmd := buildxCmd(sb, withArgs(
		"replay", "build",
		"docker-image://"+ref,
		"--builder="+name,
		"--dry-run",
		"--format=json",
	))
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	require.NoError(t, cmd.Run(), stderr.String())
	var plan struct {
		Subjects []struct {
			Descriptor ocispecs.Descriptor `json:"descriptor"`
		} `json:"subjects"`
	}
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &plan), "dry-run must emit JSON plan")
	require.Len(t, plan.Subjects, 1)
	require.NotNil(t, plan.Subjects[0].Descriptor.Platform)
	require.Equal(t, native, platforms.Format(*plan.Subjects[0].Descriptor.Platform))
}
