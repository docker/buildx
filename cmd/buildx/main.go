package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/docker/buildx/commands"
	"github.com/docker/buildx/util/cobrautil"
	"github.com/docker/buildx/util/desktop"
	"github.com/docker/buildx/version"
	"github.com/docker/cli/cli"
	"github.com/docker/cli/cli-plugins/metadata"
	"github.com/docker/cli/cli-plugins/plugin"
	"github.com/docker/cli/cli/command"
	"github.com/docker/cli/cli/debug"
	solvererrdefs "github.com/moby/buildkit/solver/errdefs"
	"github.com/moby/buildkit/sourcepolicy/policysession"
	"github.com/moby/buildkit/util/grpcerrors"
	"github.com/moby/buildkit/util/stack"
	"github.com/pkg/errors"
	"go.opentelemetry.io/otel"
	"google.golang.org/grpc/codes"

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
	return plugin.RunPlugin(cmd, rootCmd, metadata.Metadata{
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

	// Check for ExitCodeError, which is used to exit with a specific code
	// without printing an error message.
	var exitCodeErr cobrautil.ExitCodeError
	if errors.As(err, &exitCodeErr) {
		os.Exit(int(exitCodeErr))
	}
	for _, msg := range policysession.DenyMessages(err) {
		if msg.GetMessage() != "" {
			fmt.Fprintf(os.Stderr, "Policy: %s\n", msg.GetMessage())
		}
	}

	for _, s := range solvererrdefs.Sources(err) {
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
	}

	exitCode := 1
	switch grpcerrors.Code(err) {
	case codes.Internal:
		exitCode = 100 // https://github.com/square/exit/blob/v1.3.0/exit.go#L70
	case codes.ResourceExhausted:
		exitCode = 102
	case codes.Canceled:
		exitCode = 130
	default:
		if errors.Is(err, context.Canceled) {
			exitCode = 130
		}
	}

	os.Exit(exitCode)
}
