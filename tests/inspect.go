package tests

import (
	"strings"
	"testing"

	"github.com/moby/buildkit/util/testutil/integration"
	"github.com/stretchr/testify/require"
)

func inspectCmd(sb integration.Sandbox, args ...string) (string, error) {
	args = append([]string{"inspect"}, args...)
	cmd := buildxCmd(sb, args...)
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
	for _, line := range strings.Split(out, "\n") {
		if v, ok := strings.CutPrefix(line, "Name:"); ok && name == "" {
			name = strings.TrimSpace(v)
		}
		if v, ok := strings.CutPrefix(line, "Driver:"); ok && driver == "" {
			driver = strings.TrimSpace(v)
		}
	}
	require.Equal(t, sb.Address(), name)
	require.Equal(t, sb.Name(), driver)
}
