package commands

import (
	"os"

	"github.com/docker/buildx/store/storeutil"
	"github.com/docker/buildx/util/cobrautil/completion"
	"github.com/docker/buildx/util/dockerutil"
	"github.com/docker/cli/cli"
	"github.com/docker/cli/cli/command"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

type useOptions struct {
	isGlobal  bool
	isDefault bool
	builder   string
}

func runUse(dockerCli command.Cli, in useOptions) error {
	txn, release, err := storeutil.GetStore(dockerCli)
	if err != nil {
		return err
	}
	defer release()

	if _, err := txn.NodeGroupByName(in.builder); err != nil {
		if os.IsNotExist(errors.Cause(err)) {
			if in.builder == "default" && in.builder != dockerCli.CurrentContext() {
				return errors.Errorf("run `docker context use default` to switch to default context")
			}
			if in.builder == "default" || in.builder == dockerCli.CurrentContext() {
				ep, err := dockerutil.GetCurrentEndpoint(dockerCli)
				if err != nil {
					return err
				}
				return txn.SetCurrent(ep, "", false, false)
			}
			list, err := dockerCli.ContextStore().List()
			if err != nil {
				return err
			}
			for _, l := range list {
				if l.Name == in.builder {
					return errors.Errorf("run `docker context use %s` to switch to context %s", in.builder, in.builder)
				}
			}
		}
		return errors.Wrapf(err, "failed to find instance %q", in.builder)
	}

	ep, err := dockerutil.GetCurrentEndpoint(dockerCli)
	if err != nil {
		return err
	}
	return txn.SetCurrent(ep, in.builder, in.isGlobal, in.isDefault)
}

func useCmd(dockerCli command.Cli, rootOpts *rootOptions) *cobra.Command {
	var options useOptions

	cmd := &cobra.Command{
		Use:   "use [OPTIONS] NAME",
		Short: "Set the current builder instance",
		Args:  cli.RequiresMaxArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			options.builder = rootOpts.builder
			if len(args) > 0 {
				options.builder = args[0]
			}
			return runUse(dockerCli, options)
		},
		ValidArgsFunction: completion.BuilderNames(dockerCli),
	}

	flags := cmd.Flags()
	flags.BoolVar(&options.isGlobal, "global", false, "Builder persists context changes")
	flags.BoolVar(&options.isDefault, "default", false, "Set builder as default for current context")

	return cmd
}
