package tests

import (
	"fmt"
	"os"
	"path/filepath"
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
	testCreateWithProvenanceGHA,
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

	for line := range strings.SplitSeq(out, "\n") {
		if v, ok := strings.CutPrefix(line, "Status:"); ok {
			require.Equal(t, "running", strings.TrimSpace(v))
			return
		}
	}
	require.Fail(t, "remote builder is not running")
}

func testCreateWithProvenanceGHA(t *testing.T, sb integration.Sandbox) {
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

	ghep := filepath.Join(t.TempDir(), "event.json")
	require.NoError(t, os.WriteFile(ghep, []byte(`{"test":{"foo":"bar"}}`), 0644))

	out, err := createCmd(sb,
		withArgs("--driver", "docker-container"),
		withEnv("GITHUB_ACTIONS=true", "GITHUB_EVENT_NAME=push", "GITHUB_EVENT_PATH="+ghep),
	)
	require.NoError(t, err, out)
	builderName = strings.TrimSpace(out)

	out, err = inspectCmd(sb, withArgs(builderName, "--bootstrap"))
	require.NoError(t, err, out)
}
