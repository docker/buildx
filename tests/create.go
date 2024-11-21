package tests

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/docker/buildx/driver"
	"github.com/moby/buildkit/identity"
	"github.com/moby/buildkit/util/testutil/integration"
	"github.com/stretchr/testify/require"
)

func createCmd(sb integration.Sandbox, opts ...cmdOpt) (string, error) {
	opts = append([]cmdOpt{withArgs("create")}, opts...)
	cmd := buildxCmd(sb, opts...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

var createTests = []func(t *testing.T, sb integration.Sandbox){
	testCreateMemoryLimit,
	testCreateRestartAlways,
	testCreateRemoteContainer,
}

func testCreateMemoryLimit(t *testing.T, sb integration.Sandbox) {
	if !isDockerContainerWorker(sb) {
		t.Skip("only testing with docker-container worker")
	}

	var builderName string
	t.Cleanup(func() {
		if builderName == "" {
			return
		}
		out, err := rmCmd(sb, withArgs(builderName))
		require.NoError(t, err, out)
	})

	out, err := createCmd(sb, withArgs("--driver", "docker-container", "--driver-opt", "network=host", "--driver-opt", "memory=1g"))
	require.NoError(t, err, out)
	builderName = strings.TrimSpace(out)
}

func testCreateRestartAlways(t *testing.T, sb integration.Sandbox) {
	if !isDockerContainerWorker(sb) {
		t.Skip("only testing with docker-container worker")
	}

	var builderName string
	t.Cleanup(func() {
		if builderName == "" {
			return
		}
		out, err := rmCmd(sb, withArgs(builderName))
		require.NoError(t, err, out)
	})

	out, err := createCmd(sb, withArgs("--driver", "docker-container", "--driver-opt", "restart-policy=always"))
	require.NoError(t, err, out)
	builderName = strings.TrimSpace(out)
}

func testCreateRemoteContainer(t *testing.T, sb integration.Sandbox) {
	if !isDockerWorker(sb) {
		t.Skip("only testing with docker workers")
	}

	ctnBuilderName := "ctn-builder-" + identity.NewID()
	remoteBuilderName := "remote-builder-" + identity.NewID()
	var hasCtnBuilder, hasRemoteBuilder bool
	t.Cleanup(func() {
		if hasCtnBuilder {
			out, err := rmCmd(sb, withArgs(ctnBuilderName))
			require.NoError(t, err, out)
		}
		if hasRemoteBuilder {
			out, err := rmCmd(sb, withArgs(remoteBuilderName))
			require.NoError(t, err, out)
		}
	})

	out, err := createCmd(sb, withArgs("--driver", "docker-container", "--name", ctnBuilderName))
	require.NoError(t, err, out)
	hasCtnBuilder = true

	out, err = inspectCmd(sb, withArgs("--bootstrap", ctnBuilderName))
	require.NoError(t, err, out)

	cmd := dockerCmd(sb, withArgs("container", "inspect", fmt.Sprintf("%s0", driver.BuilderName(ctnBuilderName))))
	cmd.Stderr = os.Stderr
	require.NoError(t, cmd.Run())

	out, err = createCmd(sb, withArgs("--driver", "remote", "--name", remoteBuilderName, fmt.Sprintf("docker-container://%s0", driver.BuilderName(ctnBuilderName))))
	require.NoError(t, err, out)
	hasRemoteBuilder = true

	out, err = inspectCmd(sb, withArgs(remoteBuilderName))
	require.NoError(t, err, out)

	for _, line := range strings.Split(out, "\n") {
		if v, ok := strings.CutPrefix(line, "Status:"); ok {
			require.Equal(t, "running", strings.TrimSpace(v))
			return
		}
	}
	require.Fail(t, "remote builder is not running")
}
