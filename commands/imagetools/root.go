package commands

import (
	"github.com/docker/buildx/util/cobrautil"
	"github.com/docker/cli/cli/command"
	"github.com/spf13/cobra"
)

type RootOptions struct {
	Builder *string
}

func RootCmd(rootcmd *cobra.Command, dockerCli command.Cli, opts RootOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:                   "imagetools",
		Short:                 "Commands to work on images in registry",
		ValidArgsFunction:     cobrautil.DisableCompletion,
		RunE:                  rootcmd.RunE,
		DisableFlagsInUseLine: true,
	}

	cmd.AddCommand(
		createCmd(dockerCli, opts),
		inspectCmd(dockerCli, opts),
	)

	return cmd
}
