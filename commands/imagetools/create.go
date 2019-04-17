package commands

import (
	"github.com/docker/cli/cli/command"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

type createOptions struct {
	files  []string
	tags   []string
	dryrun bool
	append bool
}

func runCreate(dockerCli command.Cli, in createOptions, args []string) error {
	return errors.Errorf("not-implemented")
}

func createCmd(dockerCli command.Cli) *cobra.Command {
	var options createOptions

	cmd := &cobra.Command{
		Use:   "create [OPTIONS] [SOURCE...]",
		Short: "Create a new image based on source images",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCreate(dockerCli, options, args)
		},
	}

	flags := cmd.Flags()

	flags.StringArrayVarP(&options.files, "file", "f", []string{}, "Read source descriptor from file")
	flags.StringArrayVarP(&options.tags, "tag", "t", []string{}, "Set reference for new image")
	flags.BoolVar(&options.dryrun, "dry-run", false, "Show final image instead of pushing")
	flags.BoolVar(&options.append, "append", false, "Append to existing manifest")

	_ = flags

	return cmd
}
