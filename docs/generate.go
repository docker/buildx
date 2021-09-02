package main

import (
	"log"
	"os"
	"path/filepath"

	"github.com/docker/buildx/commands"
	clidocstool "github.com/docker/cli-docs-tool"
	"github.com/docker/cli/cli/command"
	"github.com/spf13/cobra"
)

const sourcePath = "docs/reference/"

func main() {
	log.SetFlags(0)

	dockerCLI, err := command.NewDockerCli()
	if err != nil {
		log.Printf("ERROR: %+v", err)
	}

	cmd := &cobra.Command{
		Use:               "docker [OPTIONS] COMMAND [ARG...]",
		Short:             "The base command for the Docker CLI.",
		DisableAutoGenTag: true,
	}

	cmd.AddCommand(commands.NewRootCmd("buildx", true, dockerCLI))
	clidocstool.DisableFlagsInUseLine(cmd)

	cwd, _ := os.Getwd()
	source := filepath.Join(cwd, sourcePath)

	if err = os.MkdirAll(source, 0755); err != nil {
		log.Printf("ERROR: %+v", err)
	}
	if err = clidocstool.GenTree(cmd, source); err != nil {
		log.Printf("ERROR: %+v", err)
	}
}
