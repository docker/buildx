package commands

import (
	"fmt"
	"os"

	"github.com/docker/buildx/monitor/dap"
	"github.com/docker/cli/cli/command"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

func addDAPCommands(cmd *cobra.Command, dockerCli command.Cli) {
	cmd.AddCommand(
		dapCmd(dockerCli),
		attachContainerCmd(dockerCli),
	)
}

func dapCmd(dockerCli command.Cli) *cobra.Command {
	cmd := &cobra.Command{
		Use:    "dap",
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			logrus.SetOutput(os.Stderr)
			s, err := dap.NewServer(dockerCli, os.Stdin, os.Stdout)
			if err != nil {
				return err
			}
			if err := s.Serve(); err != nil {
				logrus.WithError(err).Warnf("failed to serve")
			}
			logrus.Info("finishing server")
			return nil
		},
	}
	return cmd
}

func attachContainerCmd(dockerCli command.Cli) *cobra.Command {
	var setTtyRaw bool
	cmd := &cobra.Command{
		Use:    fmt.Sprintf("%s [OPTIONS] rootdir", dap.AttachContainerCommand),
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) < 1 || args[0] == "" {
				return errors.Errorf("specify root dir: %+v", args)
			}
			return dap.AttachContainerIO(args[0], os.Stdin, os.Stdout, os.Stderr, setTtyRaw)
		},
	}
	flags := cmd.Flags()
	flags.BoolVar(&setTtyRaw, "set-tty-raw", false, "set tty raw")
	return cmd
}
