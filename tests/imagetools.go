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
	testImagetoolsMergeSourcesWithFallbackAttestations,
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
	testImagetoolsMergeSourcesWithMode(t, sb, imagetoolsMergeNoAttestations)
}

// testImagetoolsMergeSourcesWithAttestations verifies merged sources retain attestation manifests.
func testImagetoolsMergeSourcesWithAttestations(t *testing.T, sb integration.Sandbox) {
	testImagetoolsMergeSourcesWithMode(t, sb, imagetoolsMergeInlineAttestations)
}

// testImagetoolsMergeSourcesWithFallbackAttestations verifies merged sources
// pull a copied single-platform attestation via the referrers fallback tag.
func testImagetoolsMergeSourcesWithFallbackAttestations(t *testing.T, sb integration.Sandbox) {
	testImagetoolsMergeSourcesWithMode(t, sb, imagetoolsMergeFallbackAttestations)
}

type imagetoolsMergeMode int

const (
	imagetoolsMergeNoAttestations imagetoolsMergeMode = iota
	imagetoolsMergeInlineAttestations
	imagetoolsMergeFallbackAttestations
)

func prepareSinglePlatformFallbackAsset(t *testing.T, sb integration.Sandbox, dir, registryTarget string) string {
	t.Helper()

	registrySource, err := sb.NewRegistry()
	if errors.Is(err, integration.ErrRequirements) {
		t.Skip(err.Error())
	}
	require.NoError(t, err)

	singleSource := registrySource + "/buildx/imtools-merge-single-src:latest"
	out, err := buildCmd(sb, withArgs(
		"--output", "type=image,name="+singleSource+",push=true,oci-mediatypes=true,oci-artifact=true",
		"--platform=linux/arm",
		"--provenance=mode=min",
		dir,
	))
	require.NoError(t, err, string(out))

	cmd := buildxCmd(sb, withArgs("imagetools", "inspect", singleSource, "--raw"))
	dt, err := cmd.CombinedOutput()
	require.NoError(t, err, string(dt))
	singleSourceIndexDigest := digest.FromBytes(dt)

	var singleSourceIdx ocispecs.Index
	err = json.Unmarshal(dt, &singleSourceIdx)
	require.NoError(t, err)
	require.Len(t, singleSourceIdx.Manifests, 2)

	var singleManifest, singleAttestation ocispecs.Descriptor
	for _, mfst := range singleSourceIdx.Manifests {
		if mfst.Annotations["vnd.docker.reference.type"] == "attestation-manifest" {
			singleAttestation = mfst
			continue
		}
		require.NotNil(t, mfst.Platform)
		if mfst.Platform.OS == "linux" && mfst.Platform.Architecture == "arm" {
			singleManifest = mfst
		}
	}
	require.NotEmpty(t, singleManifest.Digest)
	require.NotEmpty(t, singleAttestation.Digest)

	copiedSingle := registryTarget + "/buildx/imtools-merge-single:latest"
	cmd = buildxCmd(sb, withArgs("imagetools", "create", "--prefer-index=false", "-t", copiedSingle, singleSource+"@"+string(singleManifest.Digest)))
	dt, err = cmd.CombinedOutput()
	require.NoError(t, err, string(dt))

	cmd = buildxCmd(sb, withArgs("imagetools", "inspect", copiedSingle, "--raw"))
	dt, err = cmd.CombinedOutput()
	require.NoError(t, err, string(dt))

	var copiedManifest ocispecs.Manifest
	err = json.Unmarshal(dt, &copiedManifest)
	require.NoError(t, err)
	require.Contains(t, []string{images.MediaTypeDockerSchema2Manifest, ocispecs.MediaTypeImageManifest}, copiedManifest.MediaType)

	// no index was copied
	cmd = buildxCmd(sb, withArgs("imagetools", "inspect", copiedSingle+"@"+string(singleSourceIndexDigest), "--raw"))
	dt, err = cmd.CombinedOutput()
	require.Error(t, err, string(dt))

	cmd = buildxCmd(sb, withArgs("imagetools", "inspect", singleSource+"@"+string(singleAttestation.Digest), "--raw"))
	dt, err = cmd.CombinedOutput()
	require.NoError(t, err, string(dt))

	var attestationManifest ocispecs.Manifest
	err = json.Unmarshal(dt, &attestationManifest)
	require.NoError(t, err)
	require.NotNil(t, attestationManifest.Subject)
	require.Equal(t, singleManifest.Digest, attestationManifest.Subject.Digest)

	copiedSingleAttestation := registryTarget + "/buildx/imtools-merge-single:attestation-copy"
	cmd = buildxCmd(sb, withArgs("imagetools", "create", "--prefer-index=false", "-t", copiedSingleAttestation, singleSource+"@"+string(singleAttestation.Digest)))
	dt, err = cmd.CombinedOutput()
	require.NoError(t, err, string(dt))

	fallbackDescriptor := singleAttestation
	fallbackDescriptor.ArtifactType = "application/vnd.docker.attestation.manifest.v1+json"
	fallbackDescriptorJSON, err := json.Marshal(fallbackDescriptor)
	require.NoError(t, err)

	fallbackRef := registryTarget + "/buildx/imtools-merge-single:sha256-" + singleManifest.Digest.Encoded()
	cmd = buildxCmd(sb, withArgs("imagetools", "create", "-t", fallbackRef, string(fallbackDescriptorJSON)))
	dt, err = cmd.CombinedOutput()
	require.NoError(t, err, string(dt))

	cmd = buildxCmd(sb, withArgs("imagetools", "inspect", fallbackRef, "--raw"))
	dt, err = cmd.CombinedOutput()
	require.NoError(t, err, string(dt))

	var fallbackIdx ocispecs.Index
	err = json.Unmarshal(dt, &fallbackIdx)
	require.NoError(t, err)
	require.Len(t, fallbackIdx.Manifests, 1)
	require.Equal(t, singleAttestation.Digest, fallbackIdx.Manifests[0].Digest)
	require.Equal(t, "application/vnd.docker.attestation.manifest.v1+json", fallbackIdx.Manifests[0].ArtifactType)

	return copiedSingle
}

func testImagetoolsMergeSourcesWithMode(t *testing.T, sb integration.Sandbox, mode imagetoolsMergeMode) {
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
	registrySingleCopied, err := sb.NewRegistry()
	require.NoError(t, err)

	multiPlatformProvenanceFlag := "--provenance=false"
	if mode != imagetoolsMergeNoAttestations {
		multiPlatformProvenanceFlag = "--provenance=true"
	}

	src1 := registry1 + "/buildx/imtools-merge-1:latest"
	out, err := buildCmd(sb, withArgs("-t", src1, "--push", "--platform=linux/amd64,linux/arm64", multiPlatformProvenanceFlag, dir))
	require.NoError(t, err, string(out))

	src2 := registry2 + "/buildx/imtools-merge-2:latest"
	out, err = buildCmd(sb, withArgs("-t", src2, "--push", "--platform=linux/riscv64,linux/ppc64le", multiPlatformProvenanceFlag, dir))
	require.NoError(t, err, string(out))

	var src3 string
	switch mode {
	case imagetoolsMergeFallbackAttestations:
		src3 = prepareSinglePlatformFallbackAsset(t, sb, dir, registrySingleCopied)
	default:
		src3 = registry3 + "/buildx/imtools-merge-3:latest"
		out, err = buildCmd(sb, withArgs("-t", src3, "--push", "--platform=linux/arm", "--provenance=false", dir))
		require.NoError(t, err, string(out))
	}

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
	switch mode {
	case imagetoolsMergeInlineAttestations:
		expectedManifestCount = 9
		expectedAttestationCount = 4
	case imagetoolsMergeFallbackAttestations:
		expectedManifestCount = 10
		expectedAttestationCount = 5
	}
	assertMergedIndex(t, idx, expectedManifestCount, expectedAttestationCount)
}

func assertMergedIndex(t *testing.T, idx ocispecs.Index, expectedManifestCount, expectedAttestationCount int) {
	t.Helper()

	require.Len(t, idx.Manifests, expectedManifestCount)

	platformsFound := map[string]struct{}{}
	platformDigests := map[digest.Digest]string{}
	attestations := make([]ocispecs.Descriptor, 0, expectedAttestationCount)
	for _, mfst := range idx.Manifests {
		if mfst.Annotations["vnd.docker.reference.type"] == "attestation-manifest" {
			attestations = append(attestations, mfst)
			continue
		}
		require.NotNil(t, mfst.Platform)
		platform := mfst.Platform.OS + "/" + mfst.Platform.Architecture
		platformsFound[platform] = struct{}{}
		platformDigests[mfst.Digest] = platform
	}

	require.Len(t, attestations, expectedAttestationCount)
	for _, mfst := range attestations {
		refDigest, ok := mfst.Annotations["vnd.docker.reference.digest"]
		require.True(t, ok)
		refPlatform, ok := platformDigests[digest.Digest(refDigest)]
		require.True(t, ok, "attestation %s references unknown manifest %s", mfst.Digest, refDigest)
		require.NotEmpty(t, refPlatform)
	}

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
