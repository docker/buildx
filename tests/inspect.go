package tests

import (
	"os"
	"path"
	"regexp"
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
	testInspectBuildkitdFlags,
	testInspectNetworkHostEntitlement,
	testInspectBuildkitdConf,
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
	sbDriver, _, _ := driverName(sb.Name())
	require.Equal(t, sbDriver, driver)
	if isDockerWorker(sb) {
		require.NotEmpty(t, hostGatewayIP, "host-gateway-ip worker label should be set with docker driver")
	} else {
		require.Empty(t, hostGatewayIP, "host-gateway-ip worker label should not be set with non-docker driver")
	}
}

func testInspectBuildkitdFlags(t *testing.T, sb integration.Sandbox) {
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

	out, err := createCmd(sb, withArgs("--driver", "docker-container", "--buildkitd-flags=--oci-worker-net=bridge"))
	require.NoError(t, err, out)
	builderName = strings.TrimSpace(out)

	out, err = inspectCmd(sb, withArgs(builderName))
	require.NoError(t, err, out)

	for _, line := range strings.Split(out, "\n") {
		if v, ok := strings.CutPrefix(line, "BuildKit daemon flags:"); ok {
			require.Contains(t, v, "--oci-worker-net=bridge")
			return
		}
	}
	require.Fail(t, "--oci-worker-net=bridge not found in inspect output")
}

func testInspectNetworkHostEntitlement(t *testing.T, sb integration.Sandbox) {
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

	out, err := createCmd(sb, withArgs("--driver", "docker-container"))
	require.NoError(t, err, out)
	builderName = strings.TrimSpace(out)

	out, err = inspectCmd(sb, withArgs(builderName))
	require.NoError(t, err, out)

	for _, line := range strings.Split(out, "\n") {
		if v, ok := strings.CutPrefix(line, "BuildKit daemon flags:"); ok {
			require.Contains(t, v, "--allow-insecure-entitlement=network.host")
			return
		}
	}
	require.Fail(t, "network.host insecure entitlement not found in inspect output")
}

func testInspectBuildkitdConf(t *testing.T, sb integration.Sandbox) {
	if !isDockerContainerWorker(sb) {
		t.Skip("only testing with docker-container worker")
	}

	buildkitdConf := `
# debug enables additional debug logging
debug = true
# insecure-entitlements allows insecure entitlements, disabled by default.
insecure-entitlements = [ "network.host", "security.insecure" ]

[log]
  # log formatter: json or text
  format = "text"
`

	expectedContent := `debug = true
insecure-entitlements = ["network.host", "security.insecure"]

[log]
  format = "text"
`

	var builderName string
	t.Cleanup(func() {
		if builderName == "" {
			return
		}
		out, err := rmCmd(sb, withArgs(builderName))
		require.NoError(t, err, out)
	})

	dirConf := t.TempDir()
	buildkitdConfPath := path.Join(dirConf, "buildkitd-conf.toml")
	require.NoError(t, os.WriteFile(buildkitdConfPath, []byte(buildkitdConf), 0644))

	out, err := createCmd(sb, withArgs("--driver", "docker-container", "--buildkitd-config="+buildkitdConfPath))
	require.NoError(t, err, out)
	builderName = strings.TrimSpace(out)

	out, err = inspectCmd(sb, withArgs(builderName))
	require.NoError(t, err, out)

	var fileLines []string
	var fileFound bool
	var reConfLine = regexp.MustCompile(`^[\s\t]*>\s(.*)`)
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "File#buildkitd.toml:") {
			fileFound = true
			continue
		}
		if fileFound {
			if matches := reConfLine.FindStringSubmatch(line); len(matches) > 1 {
				fileLines = append(fileLines, matches[1])
			} else {
				break
			}
		}
	}
	if !fileFound {
		require.Fail(t, "File#buildkitd.toml not found in inspect output")
	}
	require.Equal(t, expectedContent, strings.Join(fileLines, "\n"), "File#buildkitd.toml content mismatch")
}
