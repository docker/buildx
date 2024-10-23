package tests

import (
	"os"
	"testing"

	"github.com/distribution/reference"
	"github.com/docker/buildx/tests/workers"
	"github.com/moby/buildkit/util/testutil/integration"
	bkworkers "github.com/moby/buildkit/util/testutil/workers"
)

func init() {
	if bkworkers.IsTestDockerd() {
		workers.InitDockerWorker()
		workers.InitDockerContainerWorker()
	} else {
		workers.InitRemoteWorker()
	}
}

func TestIntegration(t *testing.T) {
	var tests []func(t *testing.T, sb integration.Sandbox)
	tests = append(tests, commonTests...)
	tests = append(tests, buildTests...)
	tests = append(tests, bakeTests...)
	tests = append(tests, inspectTests...)
	tests = append(tests, lsTests...)
	tests = append(tests, imagetoolsTests...)
	tests = append(tests, versionTests...)
	tests = append(tests, createTests...)
	tests = append(tests, rmTests...)
	tests = append(tests, dialstdioTests...)
	testIntegration(t, tests...)
}

func testIntegration(t *testing.T, funcs ...func(t *testing.T, sb integration.Sandbox)) {
	mirroredImages := integration.OfficialImages("busybox:latest", "alpine:latest")
	buildkitImage = "docker.io/moby/buildkit:" + buildkitTag()
	if bkworkers.IsTestDockerd() {
		if img, ok := os.LookupEnv("TEST_BUILDKIT_IMAGE"); ok {
			ref, err := reference.ParseNormalizedNamed(img)
			if err == nil {
				buildkitImage = ref.String()
			}
		}
	}
	mirroredImages["moby/buildkit:buildx-stable-1"] = buildkitImage
	mirrors := integration.WithMirroredImages(mirroredImages)

	tests := integration.TestFuncs(funcs...)
	integration.Run(t, tests, mirrors)
}
