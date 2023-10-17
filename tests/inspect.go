package tests

import (
	"strings"
	"testing"

	"github.com/moby/buildkit/util/testutil/integration"
	"github.com/stretchr/testify/require"
)

func inspectCmd(sb integration.Sandbox, opts ...cmdOpt) (string, error) {
	opts = append([]cmdOpt{withArgs("inspect")}, opts...)
	cmd := buildxCmd(sb, opts...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

var inspectTests = []func(t *testing.T, sb integration.Sandbox){
	testInspect,
}

func testInspect(t *testing.T, sb integration.Sandbox) {
	out, err := inspectCmd(sb)
	require.NoError(t, err, string(out))

	var name string
	var driver string
	var hostGatewayIP string
	for _, line := range strings.Split(out, "\n") {
		if v, ok := strings.CutPrefix(line, "Name:"); ok && name == "" {
			name = strings.TrimSpace(v)
		}
		if v, ok := strings.CutPrefix(line, "Driver:"); ok && driver == "" {
			driver = strings.TrimSpace(v)
		}
		if v, ok := strings.CutPrefix(line, " org.mobyproject.buildkit.worker.moby.host-gateway-ip:"); ok {
			hostGatewayIP = strings.TrimSpace(v)
		}
	}

	require.Equal(t, sb.Address(), name)
	sbDriver, _, _ := strings.Cut(sb.Name(), "+")
	require.Equal(t, sbDriver, driver)
	if isDockerWorker(sb) {
		require.NotEmpty(t, hostGatewayIP, "host-gateway-ip worker label should be set with docker driver")
	} else {
		require.Empty(t, hostGatewayIP, "host-gateway-ip worker label should not be set with non-docker driver")
	}
}
