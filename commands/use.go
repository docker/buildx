package commands

import (
	"os"

	"github.com/docker/cli/cli"
	"github.com/docker/cli/cli/command"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

type useOptions struct {
	isGlobal  bool
	isDefault bool
}

func runUse(dockerCli command.Cli, in useOptions, name string) error {
	txn, release, err := getStore(dockerCli)
	if err != nil {
		return err
	}
	defer release()

	if _, err := txn.NodeGroupByName(name); err != nil {
		if os.IsNotExist(errors.Cause(err)) {
			if name == "default" || name == dockerCli.CurrentContext() {
				ep, err := getCurrentEndpoint(dockerCli)
				if err != nil {
					return err
				}
				if err := txn.SetCurrent(ep, "", false, false); err != nil {
					return err
				}
				return nil
			}
			list, err := dockerCli.ContextStore().ListContexts()
			if err != nil {
				return err
			}
			for _, l := range list {
				if l.Name == name {
					return errors.Errorf("run `docker context use %s` to switch to context %s", name, name)
				}
			}

		}
		return errors.Wrapf(err, "failed to find instance %q", name)
	}

	ep, err := getCurrentEndpoint(dockerCli)
	if err != nil {
		return err
	}
	if err := txn.SetCurrent(ep, name, in.isGlobal, in.isDefault); err != nil {
		return err
	}

	return nil
}

func useCmd(dockerCli command.Cli) *cobra.Command {
	var options useOptions

	cmd := &cobra.Command{
		Use:   "use [OPTIONS] NAME",
		Short: "Set the current builder instance",
		Args:  cli.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUse(dockerCli, options, args[0])
		},
	}

	flags := cmd.Flags()

	flags.BoolVar(&options.isGlobal, "global", false, "Builder persists context changes")
	flags.BoolVar(&options.isDefault, "default", false, "Set builder as default for current context")

	_ = flags

	return cmd
}
