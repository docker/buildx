package main

import (
	"fmt"
	"os"

	"github.com/containerd/containerd/pkg/seed"
	"github.com/docker/buildx/commands"
	"github.com/docker/buildx/version"
	"github.com/docker/cli/cli-plugins/manager"
	"github.com/docker/cli/cli-plugins/plugin"
	"github.com/docker/cli/cli/command"
	cliflags "github.com/docker/cli/cli/flags"
	"github.com/spf13/cobra"

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

	plugin.Run(func(dockerCli command.Cli) *cobra.Command {
		return commands.NewRootCmd("buildx", true, dockerCli)
	},
		manager.Metadata{
			SchemaVersion: "0.1.0",
			Vendor:        "Docker Inc.",
			Version:       version.Version,
			Experimental:  experimental != "",
		})
}
