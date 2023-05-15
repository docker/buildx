package tests

import (
	"strings"
	"testing"

	"github.com/moby/buildkit/util/testutil/integration"
	"github.com/stretchr/testify/require"
)

func lsCmd(sb integration.Sandbox, args ...string) (string, error) {
	args = append([]string{"ls"}, args...)
	cmd := buildxCmd(sb, args...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

var lsTests = []func(t *testing.T, sb integration.Sandbox){
	testLs,
}

func testLs(t *testing.T, sb integration.Sandbox) {
	out, err := lsCmd(sb)
	require.NoError(t, err, string(out))

	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, sb.Address()) {
			require.Contains(t, line, sb.Name())
			return
		}
	}
	require.Fail(t, out)
}
