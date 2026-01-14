package policy

import (
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

func jsonSchemaCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:                   "json-schema",
		Short:                 "Print policy JSON schema",
		Args:                  cobra.NoArgs,
		DisableFlagsInUseLine: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runJSONSchema()
		},
	}
	return cmd
}

func runJSONSchema() error {
	return errors.New("not implemented")
}
