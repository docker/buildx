package tests

import (
	"encoding/json"
	"testing"

	"github.com/containerd/containerd/platforms"
	"github.com/containerd/continuity/fs/fstest"
	"github.com/moby/buildkit/util/testutil/integration"
	ocispecs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"
)

var imagetoolsTests = []func(t *testing.T, sb integration.Sandbox){
	testImagetoolsInspectAndFilter,
}

func testImagetoolsInspectAndFilter(t *testing.T, sb integration.Sandbox) {
	if sb.Name() != "docker-container" {
		t.Skip("imagetools tests are not driver specific and only run on docker-container")
	}

	dockerfile := []byte(`
	FROM scratch
	ARG TARGETARCH
	COPY foo-${TARGETARCH} /foo
	`)
	dir := tmpdir(
		t,
		fstest.CreateFile("Dockerfile", dockerfile, 0600),
		fstest.CreateFile("foo-amd64", []byte("foo-amd64"), 0600),
		fstest.CreateFile("foo-arm64", []byte("foo-arm64"), 0600),
	)

	registry, err := sb.NewRegistry()
	if errors.Is(err, integration.ErrRequirements) {
		t.Skip(err.Error())
	}
	require.NoError(t, err)
	target := registry + "/buildx/imtools:latest"

	out, err := buildCmd(sb, withArgs("-t", target, "--push", "--platform=linux/amd64,linux/arm64", "--provenance=false", dir))
	require.NoError(t, err, string(out))

	cmd := buildxCmd(sb, withArgs("imagetools", "inspect", target, "--raw"))
	dt, err := cmd.CombinedOutput()
	require.NoError(t, err, string(dt))

	var idx ocispecs.Index
	err = json.Unmarshal(dt, &idx)
	require.NoError(t, err)

	require.Equal(t, 2, len(idx.Manifests))

	mfst := idx.Manifests[0]
	require.Equal(t, "linux/amd64", platforms.Format(*mfst.Platform))

	mfst = idx.Manifests[1]
	require.Equal(t, "linux/arm64", platforms.Format(*mfst.Platform))

	// create amd64 only image
	cmd = buildxCmd(sb, withArgs("imagetools", "create", "-t", target+"-arm64", target+"@"+string(idx.Manifests[1].Digest)))
	dt, err = cmd.CombinedOutput()
	require.NoError(t, err, string(dt))

	cmd = buildxCmd(sb, withArgs("imagetools", "inspect", target+"-arm64", "--raw"))
	dt, err = cmd.CombinedOutput()
	require.NoError(t, err, string(dt))

	var idx2 ocispecs.Index
	err = json.Unmarshal(dt, &idx2)
	require.NoError(t, err)

	require.Equal(t, 1, len(idx2.Manifests))

	require.Equal(t, idx.Manifests[1].Digest, idx2.Manifests[0].Digest)
	require.Equal(t, platforms.Format(*idx.Manifests[1].Platform), platforms.Format(*idx2.Manifests[0].Platform))
}
