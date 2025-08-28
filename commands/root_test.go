package commands

import (
	stderrs "errors"
	"testing"

	"github.com/docker/cli/cli/command"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func TestDisableFlagsInUseLineIsSet(t *testing.T) {
	cmd, err := command.NewDockerCli()
	require.NoError(t, err)
	rootCmd := NewRootCmd("buildx", true, cmd)

	var errs []error
	visitAll(rootCmd, func(c *cobra.Command) {
		if !c.DisableFlagsInUseLine {
			errs = append(errs, errors.New("DisableFlagsInUseLine is not set for "+c.CommandPath()))
		}
	})
	err = stderrs.Join(errs...)
	require.NoError(t, err)
}

func visitAll(root *cobra.Command, fn func(*cobra.Command)) {
	for _, cmd := range root.Commands() {
		visitAll(cmd, fn)
	}
	fn(root)
}
