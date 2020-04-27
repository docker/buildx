package commands

import (
	"os"

	imagetoolscmd "github.com/docker/buildx/commands/imagetools"
	"github.com/docker/cli/cli-plugins/plugin"
	"github.com/docker/cli/cli/command"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
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

type rootOptions struct {
	builder string
}

func addCommands(cmd *cobra.Command, dockerCli command.Cli) {
	opts := &rootOptions{}
	rootFlags(opts, cmd.PersistentFlags())

	cmd.AddCommand(
		buildCmd(dockerCli, opts),
		bakeCmd(dockerCli, opts),
		createCmd(dockerCli),
		rmCmd(dockerCli, opts),
		lsCmd(dockerCli),
		useCmd(dockerCli, opts),
		inspectCmd(dockerCli, opts),
		stopCmd(dockerCli, opts),
		installCmd(dockerCli),
		uninstallCmd(dockerCli),
		versionCmd(dockerCli),
		pruneCmd(dockerCli, opts),
		duCmd(dockerCli, opts),
		imagetoolscmd.RootCmd(dockerCli),
	)
}

func rootFlags(options *rootOptions, flags *pflag.FlagSet) {
	flags.StringVar(&options.builder, "builder", os.Getenv("BUILDX_BUILDER"), "Override the configured builder instance")
}
