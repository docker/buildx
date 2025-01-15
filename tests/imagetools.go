package tests

import (
	"encoding/json"
	"os/exec"
	"testing"

	"github.com/containerd/containerd/v2/core/images"
	"github.com/containerd/continuity/fs/fstest"
	"github.com/containerd/platforms"
	"github.com/moby/buildkit/util/testutil/integration"
	ocispecs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"
)

var imagetoolsTests = []func(t *testing.T, sb integration.Sandbox){
	testImagetoolsCopyManifest,
	testImagetoolsCopyIndex,
	testImagetoolsInspectAndFilter,
	testImagetoolsAnnotation,
}

func testImagetoolsCopyManifest(t *testing.T, sb integration.Sandbox) {
	if !isDockerContainerWorker(sb) {
		t.Skip("only testing with docker-container worker, imagetools only runs on docker-container")
	}

	dir := createDockerfile(t)
	registry, err := sb.NewRegistry()
	if errors.Is(err, integration.ErrRequirements) {
		t.Skip(err.Error())
	}
	require.NoError(t, err)
	target := registry + "/buildx/imtools-manifest:latest"

	out, err := buildCmd(sb, withArgs("-t", target, "--push", "--platform=linux/amd64", "--provenance=false", dir))
	require.NoError(t, err, string(out))

	cmd := buildxCmd(sb, withArgs("imagetools", "inspect", target, "--raw"))
	dt, err := cmd.CombinedOutput()
	require.NoError(t, err, string(dt))

	var mfst ocispecs.Manifest
	err = json.Unmarshal(dt, &mfst)
	require.NoError(t, err)
	require.Equal(t, images.MediaTypeDockerSchema2Manifest, mfst.MediaType)

	registry2, err := sb.NewRegistry()
	require.NoError(t, err)
	target2 := registry2 + "/buildx/imtools2-manifest:latest"

	cmd = buildxCmd(sb, withArgs("imagetools", "create", "-t", target2, target))
	dt, err = cmd.CombinedOutput()
	require.NoError(t, err, string(dt))

	cmd = buildxCmd(sb, withArgs("imagetools", "inspect", target2, "--raw"))
	dt, err = cmd.CombinedOutput()
	require.NoError(t, err, string(dt))

	var idx2 ocispecs.Index
	err = json.Unmarshal(dt, &idx2)
	require.NoError(t, err)
	require.Equal(t, images.MediaTypeDockerSchema2ManifestList, idx2.MediaType)
	require.Equal(t, 1, len(idx2.Manifests))

	cmd = buildxCmd(sb, withArgs("imagetools", "inspect", target2+"@"+string(idx2.Manifests[0].Digest), "--raw"))
	dt, err = cmd.CombinedOutput()
	require.NoError(t, err, string(dt))

	var mfst2 ocispecs.Manifest
	err = json.Unmarshal(dt, &mfst2)
	require.NoError(t, err)
	require.Equal(t, images.MediaTypeDockerSchema2Manifest, mfst2.MediaType)

	require.Equal(t, mfst.Config.Digest, mfst2.Config.Digest)
	require.Equal(t, len(mfst.Layers), len(mfst2.Layers))
	for i := range mfst.Layers {
		require.Equal(t, mfst.Layers[i].Digest, mfst2.Layers[i].Digest)
	}

	cmd = buildxCmd(sb, withArgs("imagetools", "create", "--prefer-index=false", "-t", target2+"-not-index", target))
	dt, err = cmd.CombinedOutput()
	require.NoError(t, err, string(dt))

	cmd = buildxCmd(sb, withArgs("imagetools", "inspect", target2+"-not-index", "--raw"))
	dt, err = cmd.CombinedOutput()
	require.NoError(t, err, string(dt))

	var idx3 ocispecs.Manifest
	err = json.Unmarshal(dt, &idx3)
	require.NoError(t, err)
	require.Equal(t, images.MediaTypeDockerSchema2Manifest, idx3.MediaType)
}

func testImagetoolsCopyIndex(t *testing.T, sb integration.Sandbox) {
	if !isDockerContainerWorker(sb) {
		t.Skip("only testing with docker-container worker, imagetools only runs on docker-container")
	}

	dir := createDockerfile(t)
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
	require.Equal(t, images.MediaTypeDockerSchema2ManifestList, idx.MediaType)
	require.Equal(t, 2, len(idx.Manifests))

	registry2, err := sb.NewRegistry()
	require.NoError(t, err)
	target2 := registry2 + "/buildx/imtools2:latest"

	cmd = buildxCmd(sb, withArgs("imagetools", "create", "-t", target2, target))
	dt, err = cmd.CombinedOutput()
	require.NoError(t, err, string(dt))

	cmd = buildxCmd(sb, withArgs("imagetools", "inspect", target2, "--raw"))
	dt, err = cmd.CombinedOutput()
	require.NoError(t, err, string(dt))

	var idx2 ocispecs.Index
	err = json.Unmarshal(dt, &idx2)
	require.NoError(t, err)
	require.Equal(t, images.MediaTypeDockerSchema2ManifestList, idx2.MediaType)

	require.Equal(t, len(idx.Manifests), len(idx2.Manifests))
	for i := range idx.Manifests {
		require.Equal(t, idx.Manifests[i].Digest, idx2.Manifests[i].Digest)
	}

	cmd = buildxCmd(sb, withArgs("imagetools", "create", "--prefer-index=false", "-t", target2+"-still-index", target))
	dt, err = cmd.CombinedOutput()
	require.NoError(t, err, string(dt))

	cmd = buildxCmd(sb, withArgs("imagetools", "inspect", target2+"-still-index", "--raw"))
	dt, err = cmd.CombinedOutput()
	require.NoError(t, err, string(dt))

	var idx3 ocispecs.Index
	err = json.Unmarshal(dt, &idx3)
	require.NoError(t, err)
	require.Equal(t, images.MediaTypeDockerSchema2ManifestList, idx3.MediaType)

	require.Equal(t, len(idx.Manifests), len(idx3.Manifests))
	for i := range idx.Manifests {
		require.Equal(t, idx.Manifests[i].Digest, idx3.Manifests[i].Digest)
	}
}

func testImagetoolsInspectAndFilter(t *testing.T, sb integration.Sandbox) {
	if !isDockerContainerWorker(sb) {
		t.Skip("only testing with docker-container worker, imagetools only runs on docker-container")
	}

	dir := createDockerfile(t)
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

func testImagetoolsAnnotation(t *testing.T, sb integration.Sandbox) {
	if !isDockerContainerWorker(sb) {
		t.Skip("only testing with docker-container worker, imagetools only runs on docker-container")
	}

	dir := createDockerfile(t)
	registry, err := sb.NewRegistry()
	if errors.Is(err, integration.ErrRequirements) {
		t.Skip(err.Error())
	}
	require.NoError(t, err)
	target := registry + "/buildx/imtools:latest"

	out, err := buildCmd(sb, withArgs("--output", "type=registry,oci-mediatypes=true,name="+target, "--platform=linux/amd64,linux/arm64", "--provenance=false", dir))
	require.NoError(t, err, string(out))

	cmd := buildxCmd(sb, withArgs("imagetools", "inspect", target, "--raw"))
	dt, err := cmd.CombinedOutput()
	require.NoError(t, err, string(dt))

	var idx ocispecs.Index
	err = json.Unmarshal(dt, &idx)
	require.NoError(t, err)
	require.Empty(t, idx.Annotations)

	imagetoolsCmd := func(source []string) *exec.Cmd {
		args := []string{"imagetools", "create", "-t", target, "--annotation", "index:foo=bar", "--annotation", "index:bar=baz",
			"--annotation", "manifest-descriptor:foo=bar", "--annotation", "manifest-descriptor[linux/amd64]:bar=baz"}
		args = append(args, source...)
		return buildxCmd(sb, withArgs(args...))
	}
	sources := [][]string{
		{
			target,
		},
		{
			target + "@" + string(idx.Manifests[0].Digest),
			target + "@" + string(idx.Manifests[1].Digest),
		},
	}
	for _, source := range sources {
		cmd = imagetoolsCmd(source)
		dt, err = cmd.CombinedOutput()
		require.NoError(t, err, string(dt))

		newTarget := registry + "/buildx/imtools:annotations"
		cmd = buildxCmd(sb, withArgs("imagetools", "create", "-t", newTarget, target))
		dt, err = cmd.CombinedOutput()
		require.NoError(t, err, string(dt))

		cmd = buildxCmd(sb, withArgs("imagetools", "inspect", newTarget, "--raw"))
		dt, err = cmd.CombinedOutput()
		require.NoError(t, err, string(dt))

		err = json.Unmarshal(dt, &idx)
		require.NoError(t, err)
		require.Len(t, idx.Annotations, 2)
		require.Equal(t, "bar", idx.Annotations["foo"])
		require.Equal(t, "baz", idx.Annotations["bar"])
		require.Len(t, idx.Manifests, 2)
		for _, mfst := range idx.Manifests {
			require.Equal(t, "bar", mfst.Annotations["foo"])
			if platforms.Format(*mfst.Platform) == "linux/amd64" {
				require.Equal(t, "baz", mfst.Annotations["bar"])
			} else {
				require.Empty(t, mfst.Annotations["bar"])
			}
		}
	}
}

func createDockerfile(t *testing.T) string {
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
	return dir
}
