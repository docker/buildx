package tests

import (
	"strings"
	"testing"

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
}

func testCreateMemoryLimit(t *testing.T, sb integration.Sandbox) {
	if sb.Name() != "docker-container" {
		t.Skip("only testing for docker-container driver")
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
	if sb.Name() != "docker-container" {
		t.Skip("only testing for docker-container driver")
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
