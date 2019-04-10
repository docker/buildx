package main

import (
	"github.com/docker/cli/cli-plugins/manager"
	"github.com/docker/cli/cli-plugins/plugin"
	"github.com/docker/cli/cli/command"
	"github.com/spf13/cobra"
	"github.com/tonistiigi/buildx/commands"
	"github.com/tonistiigi/buildx/version"

	_ "github.com/tonistiigi/buildx/driver/docker-container"
)

func main() {
	plugin.Run(func(dockerCli command.Cli) *cobra.Command {
		return commands.NewRootCmd(dockerCli)
	},
		manager.Metadata{
			SchemaVersion: "0.1.0",
			Vendor:        "Docker Inc.",
			Version:       version.Version,
		})
}
