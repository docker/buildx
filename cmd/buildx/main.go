package main

import (
	"context"
	"fmt"
	"os"

	"github.com/docker/buildx/commands"
	"github.com/docker/buildx/util/desktop"
	"github.com/docker/buildx/version"
	"github.com/docker/cli/cli"
	"github.com/docker/cli/cli-plugins/manager"
	"github.com/docker/cli/cli-plugins/plugin"
	"github.com/docker/cli/cli/command"
	"github.com/docker/cli/cli/debug"
	cliflags "github.com/docker/cli/cli/flags"
	"github.com/moby/buildkit/solver/errdefs"
	"github.com/moby/buildkit/util/stack"
	"go.opentelemetry.io/otel"

	//nolint:staticcheck // vendored dependencies may still use this
	"github.com/containerd/containerd/pkg/seed"

	_ "k8s.io/client-go/plugin/pkg/client/auth/oidc"

	_ "github.com/docker/buildx/driver/docker"
	_ "github.com/docker/buildx/driver/docker-container"
	_ "github.com/docker/buildx/driver/kubernetes"
	_ "github.com/docker/buildx/driver/remote"
)

func init() {
	//nolint:staticcheck
	seed.WithTimeAndRand()

	stack.SetVersionInfo(version.Version, version.Revision)
}

func runStandalone(cmd *command.DockerCli) error {
	if err := cmd.Initialize(cliflags.NewClientOptions()); err != nil {
		return err
	}
	defer flushMetrics(cmd)

	rootCmd := commands.NewRootCmd(os.Args[0], false, cmd)
	return rootCmd.Execute()
}

// flushMetrics will manually flush metrics from the configured
// meter provider. This is needed when running in standalone mode
// because the meter provider is initialized by the cli library,
// but the mechanism for forcing it to report is not presently
// exposed and not invoked when run in standalone mode.
// There are plans to fix that in the next release, but this is
// needed temporarily until the API for this is more thorough.
func flushMetrics(cmd *command.DockerCli) {
	if mp, ok := cmd.MeterProvider().(command.MeterProvider); ok {
		if err := mp.ForceFlush(context.Background()); err != nil {
			otel.Handle(err)
		}
	}
}

func runPlugin(cmd *command.DockerCli) error {
	rootCmd := commands.NewRootCmd("buildx", true, cmd)
	return plugin.RunPlugin(cmd, rootCmd, manager.Metadata{
		SchemaVersion: "0.1.0",
		Vendor:        "Docker Inc.",
		Version:       version.Version,
	})
}

func main() {
	cmd, err := command.NewDockerCli()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	if plugin.RunningStandalone() {
		err = runStandalone(cmd)
	} else {
		err = runPlugin(cmd)
	}
	if err == nil {
		return
	}

	if sterr, ok := err.(cli.StatusError); ok {
		if sterr.Status != "" {
			fmt.Fprintln(cmd.Err(), sterr.Status)
		}
		// StatusError should only be used for errors, and all errors should
		// have a non-zero exit status, so never exit with 0
		if sterr.StatusCode == 0 {
			os.Exit(1)
		}
		os.Exit(sterr.StatusCode)
	}

	for _, s := range errdefs.Sources(err) {
		s.Print(cmd.Err())
	}
	if debug.IsEnabled() {
		fmt.Fprintf(cmd.Err(), "ERROR: %+v", stack.Formatter(err))
	} else {
		fmt.Fprintf(cmd.Err(), "ERROR: %v\n", err)
	}
	if ebr, ok := err.(*desktop.ErrorWithBuildRef); ok {
		ebr.Print(cmd.Err())
	}

	os.Exit(1)
}
