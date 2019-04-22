package commands

import (
	"github.com/docker/cli/cli-plugins/plugin"
	"github.com/docker/cli/cli/command"
	"github.com/spf13/cobra"
	imagetoolscmd "github.com/tonistiigi/buildx/commands/imagetools"
)

func NewRootCmd(name string, isPlugin bool, dockerCli command.Cli) *cobra.Command {
	cmd := &cobra.Command{
		Short: "Build with BuildKit",
		Use:   name,
	}
	if isPlugin {
		cmd.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
			return plugin.PersistentPreRunE(cmd, args)
		}
	}

	addCommands(cmd, dockerCli)
	return cmd
}

func addCommands(cmd *cobra.Command, dockerCli command.Cli) {
	cmd.AddCommand(
		buildCmd(dockerCli),
		bakeCmd(dockerCli),
		createCmd(dockerCli),
		rmCmd(dockerCli),
		lsCmd(dockerCli),
		useCmd(dockerCli),
		inspectCmd(dockerCli),
		stopCmd(dockerCli),
		installCmd(dockerCli),
		uninstallCmd(dockerCli),
		imagetoolscmd.RootCmd(dockerCli),
	)
}
