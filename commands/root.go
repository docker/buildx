package commands

import (
	"fmt"
	"os"

	debugcmd "github.com/docker/buildx/commands/debug"
	historycmd "github.com/docker/buildx/commands/history"
	imagetoolscmd "github.com/docker/buildx/commands/imagetools"
	"github.com/docker/buildx/controller/remote"
	"github.com/docker/buildx/util/cobrautil/completion"
	"github.com/docker/buildx/util/confutil"
	"github.com/docker/buildx/util/logutil"
	"github.com/docker/cli-docs-tool/annotation"
	"github.com/docker/cli/cli"
	"github.com/docker/cli/cli-plugins/plugin"
	"github.com/docker/cli/cli/command"
	"github.com/docker/cli/cli/debug"
	"github.com/moby/buildkit/util/appcontext"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func NewRootCmd(name string, isPlugin bool, dockerCli command.Cli) *cobra.Command {
	var opt rootOptions
	cmd := &cobra.Command{
		Short: "Docker Buildx",
		Long:  `Extended build capabilities with BuildKit`,
		Use:   name,
		Annotations: map[string]string{
			annotation.CodeDelimiter: `"`,
		},
		CompletionOptions: cobra.CompletionOptions{
			HiddenDefaultCmd: true,
		},
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			if opt.debug {
				debug.Enable()
			}
			cmd.SetContext(appcontext.Context())
			if !isPlugin {
				return nil
			}
			return plugin.PersistentPreRunE(cmd, args)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}
			_ = cmd.Help()
			return cli.StatusError{
				StatusCode: 1,
				Status:     fmt.Sprintf("ERROR: unknown command: %q", args[0]),
			}
		},
	}
	if !isPlugin {
		// match plugin behavior for standalone mode
		// https://github.com/docker/cli/blob/6c9eb708fa6d17765d71965f90e1c59cea686ee9/cli-plugins/plugin/plugin.go#L117-L127
		cmd.SilenceUsage = true
		cmd.SilenceErrors = true
		cmd.TraverseChildren = true
		cmd.DisableFlagsInUseLine = true
		cli.DisableFlagsInUseLine(cmd)
	}

	logrus.SetFormatter(&logutil.Formatter{})

	logrus.AddHook(logutil.NewFilter([]logrus.Level{
		logrus.DebugLevel,
	},
		"serving grpc connection",
		"stopping session",
		"using default config store",
	))

	if !confutil.IsExperimental() {
		cmd.SetHelpTemplate(cmd.HelpTemplate() + "\nExperimental commands and flags are hidden. Set BUILDX_EXPERIMENTAL=1 to show them.\n")
	}

	addCommands(cmd, &opt, dockerCli)
	return cmd
}

type rootOptions struct {
	builder string
	debug   bool
}

func addCommands(cmd *cobra.Command, opts *rootOptions, dockerCli command.Cli) {
	rootFlags(opts, cmd.PersistentFlags())

	cmd.AddCommand(
		buildCmd(dockerCli, opts, nil),
		bakeCmd(dockerCli, opts),
		createCmd(dockerCli),
		dialStdioCmd(dockerCli, opts),
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
		imagetoolscmd.RootCmd(cmd, dockerCli, imagetoolscmd.RootOptions{Builder: &opts.builder}),
		historycmd.RootCmd(cmd, dockerCli, historycmd.RootOptions{Builder: &opts.builder}),
	)
	if confutil.IsExperimental() {
		cmd.AddCommand(debugcmd.RootCmd(dockerCli,
			newDebuggableBuild(dockerCli, opts),
		))
		remote.AddControllerCommands(cmd, dockerCli)
	}

	cmd.RegisterFlagCompletionFunc( //nolint:errcheck
		"builder",
		completion.BuilderNames(dockerCli),
	)
}

func rootFlags(options *rootOptions, flags *pflag.FlagSet) {
	flags.StringVar(&options.builder, "builder", os.Getenv("BUILDX_BUILDER"), "Override the configured builder instance")
	flags.BoolVarP(&options.debug, "debug", "D", debug.IsEnabled(), "Enable debug logging")
}
