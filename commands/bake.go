package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/docker/buildx/bake"
	"github.com/docker/buildx/build"
	"github.com/docker/buildx/util/progress"
	"github.com/docker/buildx/util/tracing"
	"github.com/docker/cli/cli/command"
	"github.com/docker/docker/pkg/ioutils"
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

func runBake(dockerCli command.Cli, targets []string, in bakeOptions) (err error) {
	ctx := appcontext.Context()

	ctx, end, err := tracing.TraceCurrentCommand(ctx, "bake")
	if err != nil {
		return err
	}
	defer func() {
		end(err)
	}()

	var url string
	cmdContext := "cwd://"

	if len(targets) > 0 {
		if bake.IsRemoteURL(targets[0]) {
			url = targets[0]
			targets = targets[1:]
			if len(targets) > 0 {
				if bake.IsRemoteURL(targets[0]) {
					cmdContext = targets[0]
					targets = targets[1:]

				}
			}
		}
	}

	if len(targets) == 0 {
		targets = []string{"default"}
	}

	overrides := in.overrides
	if in.exportPush {
		if in.exportLoad {
			return errors.Errorf("push and load may not be set together at the moment")
		}
		overrides = append(overrides, "*.output=type=registry")
	} else if in.exportLoad {
		overrides = append(overrides, "*.output=type=docker")
	}
	if in.noCache != nil {
		overrides = append(overrides, fmt.Sprintf("*.no-cache=%t", *in.noCache))
	}
	if in.pull != nil {
		overrides = append(overrides, fmt.Sprintf("*.pull=%t", *in.pull))
	}
	contextPathHash, _ := os.Getwd()

	ctx2, cancel := context.WithCancel(context.TODO())
	defer cancel()
	printer := progress.NewPrinter(ctx2, os.Stderr, in.progress)

	defer func() {
		if printer != nil {
			err1 := printer.Wait()
			if err == nil {
				err = err1
			}
		}
	}()

	dis, err := getInstanceOrDefault(ctx, dockerCli, in.builder, contextPathHash)
	if err != nil {
		return err
	}

	var files []bake.File
	var inp *bake.Input

	if url != "" {
		files, inp, err = bake.ReadRemoteFiles(ctx, dis, url, in.files, printer)
	} else {
		files, err = bake.ReadLocalFiles(in.files)
	}
	if err != nil {
		return err
	}

	m, err := bake.ReadTargets(ctx, files, targets, overrides, map[string]string{
		"BAKE_CMD_CONTEXT": cmdContext,
	})
	if err != nil {
		return err
	}

	// this function can update target context string from the input so call before printOnly check
	bo, err := bake.TargetsToBuildOpt(m, inp)
	if err != nil {
		return err
	}

	if in.printOnly {
		dt, err := json.MarshalIndent(map[string]map[string]*bake.Target{"target": m}, "", "  ")
		if err != nil {
			return err
		}
		err = printer.Wait()
		printer = nil
		if err != nil {
			return err
		}
		fmt.Fprintln(dockerCli.Out(), string(dt))
		return nil
	}

	resp, err := build.Build(ctx, dis, bo, dockerAPI(dockerCli), dockerCli.ConfigFile(), printer)
	if err != nil {
		return err
	}

	if len(in.metadataFile) > 0 && resp != nil {
		mdata := map[string]map[string]string{}
		for k, r := range resp {
			mdata[k] = r.ExporterResponse
		}
		mdatab, err := json.MarshalIndent(mdata, "", "  ")
		if err != nil {
			return err
		}
		if err := ioutils.AtomicWriteFile(in.metadataFile, mdatab, 0644); err != nil {
			return err
		}
	}

	return err
}

func bakeCmd(dockerCli command.Cli, rootOpts *rootOptions) *cobra.Command {
	var options bakeOptions

	cmd := &cobra.Command{
		Use:     "bake [OPTIONS] [TARGET...]",
		Aliases: []string{"f"},
		Short:   "Build from a file",
		RunE: func(cmd *cobra.Command, args []string) error {
			// reset to nil to avoid override is unset
			if !cmd.Flags().Lookup("no-cache").Changed {
				options.noCache = nil
			}
			if !cmd.Flags().Lookup("pull").Changed {
				options.pull = nil
			}
			options.commonOptions.builder = rootOpts.builder
			return runBake(dockerCli, args, options)
		},
	}

	flags := cmd.Flags()

	flags.StringArrayVarP(&options.files, "file", "f", []string{}, "Build definition file")
	flags.BoolVar(&options.printOnly, "print", false, "Print the options without building")
	flags.StringArrayVar(&options.overrides, "set", nil, "Override target value (eg: targetpattern.key=value)")
	flags.BoolVar(&options.exportPush, "push", false, "Shorthand for --set=*.output=type=registry")
	flags.BoolVar(&options.exportLoad, "load", false, "Shorthand for --set=*.output=type=docker")

	commonBuildFlags(&options.commonOptions, flags)

	return cmd
}
