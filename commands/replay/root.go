package replay

import (
	"github.com/docker/buildx/util/cobrautil/completion"
	"github.com/docker/cli/cli/command"
	"github.com/spf13/cobra"
)

// RootOptions mirrors the shape used by history/policy/imagetools
// (see commands/history/root.go).
type RootOptions struct {
	Builder *string
}

// commonOptions is the shared flag set for every replay subcommand.
type commonOptions struct {
	builder   string
	materials []string
	network   string
	secrets   []string
	ssh       []string
	platforms []string
	progress  string
}

// installCommonFlags registers the shared flag set on the supplied
// subcommand. Each subcommand owns its own flag registration so that
// `--help` on any leaf prints the full contract.
func installCommonFlags(cmd *cobra.Command, opts *commonOptions) {
	flags := cmd.Flags()

	flags.StringArrayVar(&opts.materials, "materials", nil, `Materials store (repeatable; format: "provenance" | "registry://<ref>" | "oci-layout://<path>[:<tag>]" | "<absolute-path>" | "<key>=<value>")`)
	flags.StringVar(&opts.network, "network", "default", `Network mode for RUN instructions ("default" | "none")`)
	flags.StringArrayVar(&opts.secrets, "secret", nil, `Secret to expose to the replayed build (format: "id=mysecret[,src=/local/secret]")`)
	flags.StringArrayVar(&opts.ssh, "ssh", nil, `SSH agent socket or keys to expose (format: "default|<id>[=<socket>|<key>[,<key>]]")`)
	flags.StringArrayVar(&opts.platforms, "platform", nil, `Subjects to replay (defaults to the current host platform; "all" keeps every platform)`)
	flags.StringVar(&opts.progress, "progress", "auto", `Set type of progress output ("auto" | "plain" | "tty" | "quiet" | "rawjson")`)
}

// RootCmd returns the `buildx replay` root command. The rootcmd argument is
// the buildx root; its RunE is reused when no subcommand is given, matching
// the pattern in commands/history/root.go.
func RootCmd(rootcmd *cobra.Command, dockerCli command.Cli, opts RootOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:               "replay",
		Short:             "Replay a build from its provenance",
		ValidArgsFunction: completion.Disable,
		RunE:              rootcmd.RunE,

		DisableFlagsInUseLine: true,
	}

	cmd.AddCommand(
		buildCmd(dockerCli, opts),
		snapshotCmd(dockerCli, opts),
		verifyCmd(dockerCli, opts),
	)

	return cmd
}
