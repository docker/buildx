package tests

import (
	"strings"
	"testing"

	"github.com/moby/buildkit/util/testutil/integration"
	"github.com/stretchr/testify/require"
)

func rmCmd(sb integration.Sandbox, opts ...cmdOpt) (string, error) {
	opts = append([]cmdOpt{withArgs("rm")}, opts...)
	cmd := buildxCmd(sb, opts...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

var rmTests = []func(t *testing.T, sb integration.Sandbox){
	testRm,
	testRmMulti,
}

func testRm(t *testing.T, sb integration.Sandbox) {
	if !isDockerContainerWorker(sb) {
		t.Skip("only testing with docker-container worker")
	}

	out, err := rmCmd(sb, withArgs("default"))
	require.Error(t, err, out) // can't remove a docker builder

	out, err = createCmd(sb, withArgs("--driver", "docker-container"))
	require.NoError(t, err, out)
	builderName := strings.TrimSpace(out)

	out, err = inspectCmd(sb, withArgs(builderName, "--bootstrap"))
	require.NoError(t, err, out)

	out, err = rmCmd(sb, withArgs(builderName))
	require.NoError(t, err, out)
}

func testRmMulti(t *testing.T, sb integration.Sandbox) {
	if !isDockerContainerWorker(sb) {
		t.Skip("only testing with docker-container worker")
	}

	var builderNames []string
	for i := 0; i < 3; i++ {
		out, err := createCmd(sb, withArgs("--driver", "docker-container"))
		require.NoError(t, err, out)
		builderName := strings.TrimSpace(out)

		out, err = inspectCmd(sb, withArgs(builderName, "--bootstrap"))
		require.NoError(t, err, out)
		builderNames = append(builderNames, builderName)
	}

	out, err := rmCmd(sb, withArgs(builderNames...))
	require.NoError(t, err, out)
}
