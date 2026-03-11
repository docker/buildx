package tests

import (
	"encoding/json"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"testing"

	"github.com/containerd/containerd/v2/core/images"
	"github.com/containerd/continuity/fs/fstest"
	"github.com/containerd/platforms"
	"github.com/moby/buildkit/util/testutil/integration"
	"github.com/opencontainers/go-digest"
	ocispecs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"
)

var imagetoolsTests = []func(t *testing.T, sb integration.Sandbox){
	testImagetoolsCopyManifest,
	testImagetoolsCopyIndex,
	testImagetoolsInspectAndFilter,
	testImagetoolsAnnotation,
	testImagetoolsMergeSources,
	testImagetoolsMergeSourcesWithAttestations,
}

// testImagetoolsCopyManifest verifies create/inspect behavior for a single-platform image.
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

	cmd = buildxCmd(sb, withArgs("imagetools", "create", "--metadata-file", path.Join(dir, "md.json"), "-t", target2, target))
	dt, err = cmd.CombinedOutput()
	require.NoError(t, err, string(dt))

	mddt, err := os.ReadFile(filepath.Join(dir, "md.json"))
	require.NoError(t, err)

	type mdT struct {
		ImageDescriptor ocispecs.Descriptor `json:"containerimage.descriptor"`
		ImageName       string              `json:"image.name"`
	}
	var md mdT
	err = json.Unmarshal(mddt, &md)
	require.NoError(t, err)
	require.NotEmpty(t, md.ImageDescriptor)
	require.Equal(t, registry2+"/buildx/imtools2-manifest", md.ImageName)

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

// testImagetoolsCopyIndex verifies create/inspect behavior for a multi-platform index.
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
	sourceDigest := digest.FromBytes(dt)

	var idx ocispecs.Index
	err = json.Unmarshal(dt, &idx)
	require.NoError(t, err)
	require.Equal(t, images.MediaTypeDockerSchema2ManifestList, idx.MediaType)
	require.Equal(t, 2, len(idx.Manifests))

	registry2, err := sb.NewRegistry()
	require.NoError(t, err)
	target2 := registry2 + "/buildx/imtools2:latest"

	cmd = buildxCmd(sb, withArgs("imagetools", "create", "--metadata-file", path.Join(dir, "md.json"), "-t", target2, target))
	dt, err = cmd.CombinedOutput()
	require.NoError(t, err, string(dt))

	mddt, err := os.ReadFile(filepath.Join(dir, "md.json"))
	require.NoError(t, err)

	type mdT struct {
		ImageDescriptor ocispecs.Descriptor `json:"containerimage.descriptor"`
		ImageName       string              `json:"image.name"`
	}
	var md mdT
	err = json.Unmarshal(mddt, &md)
	require.NoError(t, err)
	require.NotEmpty(t, md.ImageDescriptor)
	require.Equal(t, registry2+"/buildx/imtools2", md.ImageName)
	require.Equal(t, sourceDigest, md.ImageDescriptor.Digest)

	cmd = buildxCmd(sb, withArgs("imagetools", "inspect", target2, "--raw"))
	dt, err = cmd.CombinedOutput()
	require.NoError(t, err, string(dt))
	require.Equal(t, sourceDigest, digest.FromBytes(dt))

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

// testImagetoolsInspectAndFilter verifies inspect output and digest-based platform selection.
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

	// create arm64 image only
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

// testImagetoolsAnnotation verifies index and manifest annotations added by imagetools create.
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

// testImagetoolsMergeSources verifies create merges manifests from distinct source registries.
func testImagetoolsMergeSources(t *testing.T, sb integration.Sandbox) {
	testImagetoolsMergeSourcesWithOptions(t, sb, false)
}

// testImagetoolsMergeSourcesWithAttestations verifies merged sources retain attestation manifests.
func testImagetoolsMergeSourcesWithAttestations(t *testing.T, sb integration.Sandbox) {
	testImagetoolsMergeSourcesWithOptions(t, sb, true)
}

func testImagetoolsMergeSourcesWithOptions(t *testing.T, sb integration.Sandbox, withAttestations bool) {
	if !isDockerContainerWorker(sb) {
		t.Skip("only testing with docker-container worker, imagetools only runs on docker-container")
	}

	dir := createDockerfileWithArches(t, "amd64", "arm64", "riscv64", "ppc64le", "arm")

	registry1, err := sb.NewRegistry()
	if errors.Is(err, integration.ErrRequirements) {
		t.Skip(err.Error())
	}
	require.NoError(t, err)
	registry2, err := sb.NewRegistry()
	require.NoError(t, err)
	registry3, err := sb.NewRegistry()
	require.NoError(t, err)
	registryMerged, err := sb.NewRegistry()
	require.NoError(t, err)

	multiPlatformProvenanceFlag := "--provenance=false"
	if withAttestations {
		multiPlatformProvenanceFlag = "--provenance=true"
	}

	src1 := registry1 + "/buildx/imtools-merge-1:latest"
	out, err := buildCmd(sb, withArgs("-t", src1, "--push", "--platform=linux/amd64,linux/arm64", multiPlatformProvenanceFlag, dir))
	require.NoError(t, err, string(out))

	src2 := registry2 + "/buildx/imtools-merge-2:latest"
	out, err = buildCmd(sb, withArgs("-t", src2, "--push", "--platform=linux/riscv64,linux/ppc64le", multiPlatformProvenanceFlag, dir))
	require.NoError(t, err, string(out))

	src3 := registry3 + "/buildx/imtools-merge-3:latest"
	out, err = buildCmd(sb, withArgs("-t", src3, "--push", "--platform=linux/arm", "--provenance=false", dir))
	require.NoError(t, err, string(out))

	merged := registryMerged + "/buildx/imtools-merge:latest"
	cmd := buildxCmd(sb, withArgs("imagetools", "create", "-t", merged, src1, src2, src3))
	dt, err := cmd.CombinedOutput()
	require.NoError(t, err, string(dt))

	cmd = buildxCmd(sb, withArgs("imagetools", "inspect", merged, "--raw"))
	dt, err = cmd.CombinedOutput()
	require.NoError(t, err, string(dt))

	var idx ocispecs.Index
	err = json.Unmarshal(dt, &idx)
	require.NoError(t, err)

	expectedManifestCount := 5
	expectedAttestationCount := 0
	if withAttestations {
		expectedManifestCount = 9
		expectedAttestationCount = 4
	}
	require.Len(t, idx.Manifests, expectedManifestCount)

	platformsFound := map[string]struct{}{}
	platformDigests := map[digest.Digest]string{}
	attestationCount := 0
	for _, mfst := range idx.Manifests {
		if mfst.Annotations["vnd.docker.reference.type"] == "attestation-manifest" {
			refDigest, ok := mfst.Annotations["vnd.docker.reference.digest"]
			require.True(t, ok)
			refPlatform, ok := platformDigests[digest.Digest(refDigest)]
			require.True(t, ok, "attestation %s references unknown manifest %s", mfst.Digest, refDigest)
			require.NotEmpty(t, refPlatform)
			attestationCount++
			continue
		}
		require.NotNil(t, mfst.Platform)
		platform := mfst.Platform.OS + "/" + mfst.Platform.Architecture
		platformsFound[platform] = struct{}{}
		platformDigests[mfst.Digest] = platform
	}

	require.Equal(t, expectedAttestationCount, attestationCount)
	require.Len(t, platformsFound, 5)
	for _, p := range []string{"linux/amd64", "linux/arm64", "linux/riscv64", "linux/ppc64le", "linux/arm"} {
		_, ok := platformsFound[p]
		require.True(t, ok, "missing merged platform %s", p)
	}
}

func createDockerfile(t *testing.T) string {
	return createDockerfileWithArches(t, "amd64", "arm64")
}

func createDockerfileWithArches(t *testing.T, archs ...string) string {
	dockerfile := []byte(`
	FROM scratch
	ARG TARGETARCH
	COPY foo-${TARGETARCH} /foo
	`)
	appliers := []fstest.Applier{
		fstest.CreateFile("Dockerfile", dockerfile, 0600),
	}
	for _, arch := range archs {
		appliers = append(appliers, fstest.CreateFile("foo-"+arch, []byte("foo-"+arch), 0600))
	}
	dir := tmpdir(t, appliers...)
	return dir
}
