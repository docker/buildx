package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/docker/buildx/commands"
	controllererrors "github.com/docker/buildx/controller/errdefs"
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
	"github.com/pkg/errors"
	"go.opentelemetry.io/otel"

	_ "k8s.io/client-go/plugin/pkg/client/auth/oidc"

	_ "github.com/docker/buildx/driver/docker"
	_ "github.com/docker/buildx/driver/docker-container"
	_ "github.com/docker/buildx/driver/kubernetes"
	_ "github.com/docker/buildx/driver/remote"

	// Use custom grpc codec to utilize vtprotobuf
	_ "github.com/moby/buildkit/util/grpcutil/encoding/proto"
)

func init() {
	stack.SetVersionInfo(version.Version, version.Revision)
}

func runStandalone(cmd *command.DockerCli) error {
	if err := cmd.Initialize(cliflags.NewClientOptions()); err != nil {
		return err
	}
	defer flushMetrics(cmd)

	executable := os.Args[0]
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		// Note that we're cutting some corners here. The intent here
		// is for both "usage" and shell-completion scripts to use the
		// name of the executable with its actual (lower, upper) case.
		// However, on case-insensitive platforms, the user can invoke
		// the executable using any case (e.g., "bUiLdX").
		//
		// Unfortunately, neither [os.Executable] nor [os.Stat] provide
		// this information, and the "correct" solution would be to use
		// [os.File.Readdirnames], which is a rather heavy hammer to use
		// just for this.
		//
		// So, on macOS and Windows (usually case-insensitive platforms)
		// we assume the executable is always lower-case, but it's worth
		// noting that there's a corner-case to this corner-case; both
		// Windows and macOS can be configured to use a case-sensitive
		// filesystem (on Windows, this can be configured per-Directory).
		// If that is the case, and the executable is not lowercase, the
		// generated shell-completion script will be invalid.
		//
		// Let's assume that's not the case, and that the user did not
		// rename the executable to anything uppercase.
		executable = strings.ToLower(executable)
	}
	rootCmd := commands.NewRootCmd(filepath.Base(executable), false, cmd)
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

func run(cmd *command.DockerCli) error {
	stopProfiles := setupDebugProfiles(context.TODO())
	defer stopProfiles()

	if plugin.RunningStandalone() {
		return runStandalone(cmd)
	}
	return runPlugin(cmd)
}

func main() {
	cmd, err := command.NewDockerCli()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	if err = run(cmd); err == nil {
		return
	}

	// Check the error from the run function above.
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

	var ebr *desktop.ErrorWithBuildRef
	if errors.As(err, &ebr) {
		ebr.Print(cmd.Err())
	} else {
		var be *controllererrors.BuildError
		if errors.As(err, &be) {
			be.PrintBuildDetails(cmd.Err())
		}
	}

	os.Exit(1)
}
