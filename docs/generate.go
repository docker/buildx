package main

import (
	"log"
	"os"
	"path/filepath"

	"github.com/docker/buildx/commands"
	clidocstool "github.com/docker/cli-docs-tool"
	"github.com/docker/cli/cli/command"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

const defaultSourcePath = "docs/reference/"

type options struct {
	source  string
	formats []string
}

func gen(opts *options) error {
	log.SetFlags(0)

	dockerCLI, err := command.NewDockerCli()
	if err != nil {
		return err
	}
	cmd := &cobra.Command{
		Use:               "docker [OPTIONS] COMMAND [ARG...]",
		Short:             "The base command for the Docker CLI.",
		DisableAutoGenTag: true,
	}

	cmd.AddCommand(commands.NewRootCmd("buildx", true, dockerCLI))
	clidocstool.DisableFlagsInUseLine(cmd)

	cwd, _ := os.Getwd()
	source := filepath.Join(cwd, opts.source)

	if err = os.MkdirAll(source, 0755); err != nil {
		return err
	}

	for _, format := range opts.formats {
		switch format {
		case "md":
			if err = clidocstool.GenMarkdownTree(cmd, source); err != nil {
				return err
			}
		case "yaml":
			if err = clidocstool.GenYamlTree(cmd, source); err != nil {
				return err
			}
		default:
			return errors.Errorf("unknwown doc format %q", format)
		}
	}

	return nil
}

func run() error {
	opts := &options{}
	flags := pflag.NewFlagSet(os.Args[0], pflag.ContinueOnError)
	flags.StringVar(&opts.source, "source", defaultSourcePath, "Docs source folder")
	flags.StringSliceVar(&opts.formats, "formats", []string{}, "Format (md, yaml)")
	if err := flags.Parse(os.Args[1:]); err != nil {
		return err
	}
	if len(opts.formats) == 0 {
		return errors.New("Docs format required")
	}
	return gen(opts)
}

func main() {
	if err := run(); err != nil {
		log.Printf("ERROR: %+v", err)
		os.Exit(1)
	}
}
