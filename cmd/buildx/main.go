package main

import (
	"fmt"
	"os"

	"github.com/containerd/containerd/pkg/seed"
	"github.com/docker/buildx/commands"
	"github.com/docker/buildx/version"
	"github.com/docker/cli/cli"
	"github.com/docker/cli/cli-plugins/manager"
	"github.com/docker/cli/cli-plugins/plugin"
	"github.com/docker/cli/cli/command"
	"github.com/docker/cli/cli/debug"
	cliflags "github.com/docker/cli/cli/flags"
	"github.com/moby/buildkit/solver/errdefs"
	"github.com/moby/buildkit/util/stack"
	"github.com/moby/buildkit/util/tracing/detect"
	"go.opentelemetry.io/otel"

	_ "github.com/moby/buildkit/util/tracing/detect/delegated"
	_ "github.com/moby/buildkit/util/tracing/env"

	// FIXME: "k8s.io/client-go/plugin/pkg/client/auth/azure" is excluded because of compilation error
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	_ "k8s.io/client-go/plugin/pkg/client/auth/oidc"
	_ "k8s.io/client-go/plugin/pkg/client/auth/openstack"

	_ "github.com/docker/buildx/driver/docker"
	_ "github.com/docker/buildx/driver/docker-container"
	_ "github.com/docker/buildx/driver/kubernetes"
)

var experimental string

func init() {
	seed.WithTimeAndRand()
	stack.SetVersionInfo(version.Version, version.Revision)

	detect.ServiceName = "buildx"
	// do not log tracing errors to stdio
	otel.SetErrorHandler(skipErrors{})
}

func main() {
	if os.Getenv("DOCKER_CLI_PLUGIN_ORIGINAL_CLI_COMMAND") == "" {
		if len(os.Args) < 2 || os.Args[1] != manager.MetadataSubcommandName {
			dockerCli, err := command.NewDockerCli()
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			opts := cliflags.NewClientOptions()
			dockerCli.Initialize(opts)
			rootCmd := commands.NewRootCmd(os.Args[0], false, dockerCli)
			if err := rootCmd.Execute(); err != nil {
				os.Exit(1)
			}
			os.Exit(0)
		}
	}

	dockerCli, err := command.NewDockerCli()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	p := commands.NewRootCmd("buildx", true, dockerCli)
	meta := manager.Metadata{
		SchemaVersion: "0.1.0",
		Vendor:        "Docker Inc.",
		Version:       version.Version,
		Experimental:  experimental != "",
	}

	if err := plugin.RunPlugin(dockerCli, p, meta); err != nil {
		if sterr, ok := err.(cli.StatusError); ok {
			if sterr.Status != "" {
				fmt.Fprintln(dockerCli.Err(), sterr.Status)
			}
			// StatusError should only be used for errors, and all errors should
			// have a non-zero exit status, so never exit with 0
			if sterr.StatusCode == 0 {
				os.Exit(1)
			}
			os.Exit(sterr.StatusCode)
		}
		for _, s := range errdefs.Sources(err) {
			s.Print(dockerCli.Err())
		}

		if debug.IsEnabled() {
			fmt.Fprintf(dockerCli.Err(), "error: %+v", stack.Formatter(err))
		} else {
			fmt.Fprintf(dockerCli.Err(), "error: %v\n", err)
		}

		os.Exit(1)
	}
}

type skipErrors struct{}

func (skipErrors) Handle(err error) {}
