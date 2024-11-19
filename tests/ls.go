package tests

import (
	"strings"
	"testing"

	"github.com/moby/buildkit/util/testutil/integration"
	"github.com/stretchr/testify/require"
)

func lsCmd(sb integration.Sandbox, opts ...cmdOpt) (string, error) {
	opts = append([]cmdOpt{withArgs("ls")}, opts...)
	cmd := buildxCmd(sb, opts...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

var lsTests = []func(t *testing.T, sb integration.Sandbox){
	testLs,
}

func testLs(t *testing.T, sb integration.Sandbox) {
	tests := []struct {
		name string
		args []string
	}{
		{
			name: "no args",
			args: []string{},
		},
		{
			name: "format",
			args: []string{"--format", "{{.Name}}: {{.DriverEndpoint}}"},
		},
	}

	sbDriver, _, _ := driverName(sb.Name())
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			out, err := lsCmd(sb, withArgs(tt.args...))
			require.NoError(t, err, out)
			found := false
			for _, line := range strings.Split(out, "\n") {
				if strings.Contains(line, sb.Address()) {
					found = true
					require.Contains(t, line, sbDriver)
					break
				}
			}
			if !found {
				require.Fail(t, out)
			}
		})
	}
}
