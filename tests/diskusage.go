package tests

import (
	"testing"

	"github.com/moby/buildkit/util/testutil/integration"
	"github.com/stretchr/testify/require"
)

var diskusageTests = []func(t *testing.T, sb integration.Sandbox){
	testPruneSummary,
	testDiskusage,
	testDiskusageFilterUntil,
	testDiskusageVerbose,
	testDiskusageVerboseFormatError,
	testDiskusageFormatJSON,
	testDiskusageFormatGoTemplate,
}

func testPruneSummary(t *testing.T, sb integration.Sandbox) {
	for _, verbose := range []bool{false, true} {
		args := []string{"prune", "--force", "--filter=until=24h"}
		if verbose {
			args = append(args, "--verbose")
		}
		cmd := buildxCmd(sb, withArgs(args...))
		out, err := cmd.Output()
		require.NoError(t, err, string(out))
		require.Equal(t, "Total reclaimed space: 0B\n", string(out))
	}
}

func testDiskusage(t *testing.T, sb integration.Sandbox) {
	buildTestProject(t, sb)
	cmd := buildxCmd(sb, withArgs("du"))
	out, err := cmd.Output()
	require.NoError(t, err, string(out))
}

func testDiskusageFilterUntil(t *testing.T, sb integration.Sandbox) {
	buildTestProject(t, sb)
	cmd := buildxCmd(sb, withArgs("du", "--filter=until=24h"))
	out, err := cmd.Output()
	require.NoError(t, err, string(out))
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
