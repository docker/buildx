package commands

import (
	"context"
	"os"
	"runtime"

	"github.com/containerd/console"
	"github.com/docker/buildx/controller"
	"github.com/docker/buildx/controller/control"
	controllerapi "github.com/docker/buildx/controller/pb"
	"github.com/docker/buildx/monitor"
	"github.com/docker/buildx/util/progress"
	"github.com/docker/cli/cli/command"
	"github.com/moby/buildkit/util/progress/progressui"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

func debugShellCmd(dockerCli command.Cli) *cobra.Command {
	var options control.ControlOptions
	var progressMode string

	cmd := &cobra.Command{
		Use:   "debug-shell",
		Short: "Start a monitor",
		Annotations: map[string]string{
			"experimentalCLI": "",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			printer, err := progress.NewPrinter(context.TODO(), os.Stderr, progressui.DisplayMode(progressMode))
			if err != nil {
				return err
			}

			ctx := context.TODO()
			c, err := controller.NewController(ctx, options, dockerCli, printer)
			if err != nil {
				return err
			}
			defer func() {
				if err := c.Close(); err != nil {
					logrus.Warnf("failed to close server connection %v", err)
				}
			}()
			con := console.Current()
			if err := con.SetRaw(); err != nil {
				return errors.Errorf("failed to configure terminal: %v", err)
			}

			err = monitor.RunMonitor(ctx, "", nil, controllerapi.InvokeConfig{
				Tty: true,
			}, c, dockerCli.In(), os.Stdout, os.Stderr, printer)
			con.Reset()
			return err
		},
	}

	flags := cmd.Flags()

	flags.StringVar(&options.Root, "root", "", "Specify root directory of server to connect")
	flags.SetAnnotation("root", "experimentalCLI", nil)

	flags.BoolVar(&options.Detach, "detach", runtime.GOOS == "linux", "Detach buildx server (supported only on linux)")
	flags.SetAnnotation("detach", "experimentalCLI", nil)

	flags.StringVar(&options.ServerConfig, "server-config", "", "Specify buildx server config file (used only when launching new server)")
	flags.SetAnnotation("server-config", "experimentalCLI", nil)

	flags.StringVar(&progressMode, "progress", "auto", `Set type of progress output ("auto", "plain", "tty"). Use plain to show container output`)

	return cmd
}

func addDebugShellCommand(cmd *cobra.Command, dockerCli command.Cli) {
	cmd.AddCommand(
		debugShellCmd(dockerCli),
	)
}
