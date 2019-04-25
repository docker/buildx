package commands

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/docker/buildx/bake"
	"github.com/docker/cli/cli/command"
	"github.com/moby/buildkit/util/appcontext"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

type bakeOptions struct {
	files     []string
	printOnly bool
	overrides []string
	commonOptions
}

func runBake(dockerCli command.Cli, targets []string, in bakeOptions) error {
	ctx := appcontext.Context()

	if len(in.files) == 0 {
		files, err := defaultFiles()
		if err != nil {
			return err
		}
		if len(files) == 0 {
			return errors.Errorf("no docker-compose.yml or dockerbuild.hcl found, specify build file with -f/--file")
		}
		in.files = files
	}

	if len(targets) == 0 {
		targets = []string{"default"}
	}

	m, err := bake.ReadTargets(ctx, in.files, targets, in.overrides)
	if err != nil {
		return err
	}

	if in.printOnly {
		dt, err := json.MarshalIndent(map[string]map[string]bake.Target{"target": m}, "", "   ")
		if err != nil {
			return err
		}
		fmt.Fprintln(dockerCli.Out(), string(dt))
		return nil
	}

	bo, err := bake.TargetsToBuildOpt(m)
	if err != nil {
		return err
	}

	return buildTargets(ctx, dockerCli, bo, in.progress)
}

func defaultFiles() ([]string, error) {
	fns := []string{
		"docker-compose.yml",  // support app
		"docker-compose.yaml", // support app
		"docker-bake.json",
		"docker-bake.override.json",
		"docker-bake.hcl",
		"docker-bake.override.hcl",
	}
	out := make([]string, 0, len(fns))
	for _, f := range fns {
		if _, err := os.Stat(f); err != nil {
			if os.IsNotExist(errors.Cause(err)) {
				continue
			}
			return nil, err
		}
		out = append(out, f)
	}
	return out, nil
}

func bakeCmd(dockerCli command.Cli) *cobra.Command {
	var options bakeOptions

	cmd := &cobra.Command{
		Use:     "bake [OPTIONS] [TARGET...]",
		Aliases: []string{"f"},
		Short:   "Build from a file",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBake(dockerCli, args, options)
		},
	}

	flags := cmd.Flags()

	flags.StringArrayVarP(&options.files, "file", "f", []string{}, "Build definition file")
	flags.BoolVar(&options.printOnly, "print", false, "Print the options without building")
	flags.StringArrayVar(&options.overrides, "set", nil, "Override target value (eg: target.key=value)")

	commonFlags(&options.commonOptions, flags)

	return cmd
}
