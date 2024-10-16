package tests

import (
	"testing"

	"github.com/moby/buildkit/util/testutil/integration"
	"github.com/stretchr/testify/require"
)

var commonTests = []func(t *testing.T, sb integration.Sandbox){
	testUnknownCommand,
	testUnknownFlag,
}

func testUnknownCommand(t *testing.T, sb integration.Sandbox) {
	cmd := buildxCmd(sb, withArgs("foo"))
	out, err := cmd.CombinedOutput()
	require.Error(t, err, string(out))

	cmd = buildxCmd(sb, withArgs("imagetools", "foo"))
	out, err = cmd.CombinedOutput()
	require.Error(t, err, string(out))
}

func testUnknownFlag(t *testing.T, sb integration.Sandbox) {
	cmd := buildxCmd(sb, withArgs("build", "--foo=bar"))
	out, err := cmd.CombinedOutput()
	require.Error(t, err, string(out))
}
