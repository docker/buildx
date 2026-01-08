package policy

import (
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

func testCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "test <path>",
		Short: "Run policy tests",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTest(args[0])
		},
	}
	return cmd
}

func runTest(path string) error {
	_ = path
	return errors.New("not implemented")
}
