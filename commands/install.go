package commands

import (
	"os"

	"github.com/docker/cli/cli"
	"github.com/docker/cli/cli/command"
	"github.com/docker/cli/cli/config"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

type installOptions struct {
}

func runInstall(dockerCli command.Cli, in installOptions) error {
	dir := config.Dir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return errors.Wrap(err, "could not create docker config")
	}

	cfg, err := config.Load(dir)
	if err != nil {
		return err
	}

	if cfg.Aliases == nil {
		cfg.Aliases = map[string]string{}
	}
	cfg.Aliases["builder"] = "buildx"

	if err := cfg.Save(); err != nil {
		return errors.Wrap(err, "could not write docker config")
	}
	return nil
}

func installCmd(dockerCli command.Cli) *cobra.Command {
	var options installOptions

	cmd := &cobra.Command{
		Use:   "install",
		Short: "Install buildx as a 'docker builder' alias",
		Args:  cli.ExactArgs(0),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInstall(dockerCli, options)
		},
		Hidden: true,
	}

	return cmd
}
