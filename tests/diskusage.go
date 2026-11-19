package tests

import (
	"testing"

	"github.com/moby/buildkit/util/testutil/integration"
	"github.com/stretchr/testify/require"
)

var diskusageTests = []func(t *testing.T, sb integration.Sandbox){
	testDiskusage,
	testDiskusageFilterUntil,
	testDiskusageVerbose,
	testDiskusageVerboseFormatError,
	testDiskusageFormatJSON,
	testDiskusageFormatGoTemplate,
}

func testDiskusage(t *testing.T, sb integration.Sandbox) {
	buildTestProject(t, sb)
	cmd := buildxCmd(sb, withArgs("du"))
	out, err := cmd.Output()
	require.NoError(t, err, string(out))
}

func testDiskusageFilterUntil(t *testing.T, sb integration.Sandbox) {
	skipNoCompatBuildKit(t, sb, ">= 0.22.0-0", "disk usage age filter")
	buildTestProject(t, sb)
	cmd := buildxCmd(sb, withArgs("du", "--format={{.ID}}", "--filter=type=source.local"))
	out, err := cmd.Output()
	require.NoError(t, err, string(out))
	require.NotEmpty(t, out)
	for _, filter := range []string{"until=24h", "unused-for=24h"} {
		t.Run(filter, func(t *testing.T) {
			cmd := buildxCmd(sb, withArgs("du", "--format={{.ID}}", "--filter=type=source.local", "--filter="+filter))
			out, err := cmd.Output()
			require.NoError(t, err, string(out))
			require.Empty(t, out)
		})
	}
}

func testDiskusageVerbose(t *testing.T, sb integration.Sandbox) {
	buildTestProject(t, sb)
	cmd := buildxCmd(sb, withArgs("du", "--verbose"))
	out, err := cmd.Output()
	require.NoError(t, err, string(out))
}

func testDiskusageVerboseFormatError(t *testing.T, sb integration.Sandbox) {
	buildTestProject(t, sb)
	cmd := buildxCmd(sb, withArgs("du", "--verbose", "--format=json"))
	out, err := cmd.Output()
	require.Error(t, err, string(out))
}

func testDiskusageFormatJSON(t *testing.T, sb integration.Sandbox) {
	buildTestProject(t, sb)
	cmd := buildxCmd(sb, withArgs("du", "--format=json"))
	out, err := cmd.Output()
	require.NoError(t, err, string(out))
}

func testDiskusageFormatGoTemplate(t *testing.T, sb integration.Sandbox) {
	buildTestProject(t, sb)
	cmd := buildxCmd(sb, withArgs("du", "--format={{.ID}}: {{.Size}}"))
	out, err := cmd.Output()
	require.NoError(t, err, string(out))
}
