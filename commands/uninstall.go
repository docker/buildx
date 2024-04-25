package commands

import (
	"os"

	"github.com/docker/buildx/util/cobrautil"
	"github.com/docker/buildx/util/cobrautil/completion"
	"github.com/docker/cli/cli"
	"github.com/docker/cli/cli/command"
	"github.com/docker/cli/cli/config"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

type uninstallOptions struct {
}

func runUninstall(_ command.Cli, _ uninstallOptions) error {
	dir := config.Dir()
	cfg, err := config.Load(dir)
	if err != nil {
		return errors.Wrap(err, "could not load docker config to uninstall 'docker builder' alias")
	}
	// config.Load does not return an error if config file does not exist
	// so let's detect that case, to avoid writing an empty config to disk.
	if _, err := os.Stat(cfg.Filename); err != nil {
		if !os.IsNotExist(err) {
			// should never happen, already handled in config.Load
			return errors.Wrap(err, "unexpected error loading docker config")
		}
		// no-op
		return nil
	}

	delete(cfg.Aliases, "builder")
	if len(cfg.Aliases) == 0 {
		cfg.Aliases = nil
	}

	if err := cfg.Save(); err != nil {
		return errors.Wrap(err, "could not write docker config")
	}
	return nil
}

func uninstallCmd(dockerCli command.Cli) *cobra.Command {
	var options uninstallOptions

	cmd := &cobra.Command{
		Use:   "uninstall",
		Short: "Uninstall the 'docker builder' alias",
		Args:  cli.ExactArgs(0),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUninstall(dockerCli, options)
		},
		Hidden:            true,
		ValidArgsFunction: completion.Disable,
	}

	// hide builder persistent flag for this command
	cobrautil.HideInheritedFlags(cmd, "builder")

	return cmd
}
