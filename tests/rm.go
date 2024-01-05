package tests

import (
	"github.com/moby/buildkit/util/testutil/integration"
)

func rmCmd(sb integration.Sandbox, opts ...cmdOpt) (string, error) {
	opts = append([]cmdOpt{withArgs("rm")}, opts...)
	cmd := buildxCmd(sb, opts...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}
