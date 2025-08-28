package history

import (
	"github.com/docker/buildx/util/cobrautil/completion"
	"github.com/docker/cli/cli/command"
	"github.com/spf13/cobra"
)

type RootOptions struct {
	Builder *string
}

func RootCmd(rootcmd *cobra.Command, dockerCli command.Cli, opts RootOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:               "history",
		Short:             "Commands to work on build records",
		ValidArgsFunction: completion.Disable,
		RunE:              rootcmd.RunE,

		DisableFlagsInUseLine: true,
	}

	cmd.AddCommand(
		lsCmd(dockerCli, opts),
		rmCmd(dockerCli, opts),
		logsCmd(dockerCli, opts),
		inspectCmd(dockerCli, opts),
		openCmd(dockerCli, opts),
		traceCmd(dockerCli, opts),
		importCmd(dockerCli, opts),
		exportCmd(dockerCli, opts),
	)

	return cmd
}
