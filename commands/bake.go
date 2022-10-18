package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/containerd/containerd/platforms"
	"github.com/docker/buildx/bake"
	"github.com/docker/buildx/build"
	"github.com/docker/buildx/util/confutil"
	"github.com/docker/buildx/util/progress"
	"github.com/docker/buildx/util/tracing"
	"github.com/docker/cli/cli/command"
	"github.com/moby/buildkit/util/appcontext"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

type bakeOptions struct {
	files     []string
	overrides []string
	printOnly bool
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
		overrides = append(overrides, "*.push=true")
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
	printer := progress.NewPrinter(ctx2, os.Stderr, os.Stderr, in.progress)

	defer func() {
		if printer != nil {
			err1 := printer.Wait()
			if err == nil {
				err = err1
			}
		}
	}()

	var dis []build.DriverInfo
	var files []bake.File
	var inp *bake.Input

	// instance only needed for reading remote bake files or building
	if url != "" || !in.printOnly {
		dis, err = getInstanceOrDefault(ctx, dockerCli, in.builder, contextPathHash)
		if err != nil {
			return err
		}
	}

	if url != "" {
		files, inp, err = bake.ReadRemoteFiles(ctx, dis, url, in.files, printer)
	} else {
		files, err = bake.ReadLocalFiles(in.files)
	}
	if err != nil {
		return err
	}

	tgts, grps, err := bake.ReadTargets(ctx, files, targets, overrides, map[string]string{
		// don't forget to update documentation if you add a new
		// built-in variable: docs/guides/bake/file-definition.md#built-in-variables
		"BAKE_CMD_CONTEXT":    cmdContext,
		"BAKE_LOCAL_PLATFORM": platforms.DefaultString(),
	})
	if err != nil {
		return err
	}

	// this function can update target context string from the input so call before printOnly check
	bo, err := bake.TargetsToBuildOpt(tgts, inp)
	if err != nil {
		return err
	}

	if in.printOnly {
		var defg map[string]*bake.Group
		if len(grps) == 1 {
			defg = map[string]*bake.Group{
				"default": grps[0],
			}
		}
		dt, err := json.MarshalIndent(struct {
			Group  map[string]*bake.Group  `json:"group,omitempty"`
			Target map[string]*bake.Target `json:"target"`
		}{
			defg,
			tgts,
		}, "", "  ")
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

	resp, err := build.Build(ctx, dis, bo, dockerAPI(dockerCli), confutil.ConfigDir(dockerCli), printer)
	if err != nil {
		return wrapBuildError(err, true)
	}

	if len(in.metadataFile) > 0 {
		dt := make(map[string]interface{})
		for t, r := range resp {
			dt[t] = decodeExporterResponse(r.ExporterResponse)
		}
		if err := writeMetadataFile(in.metadataFile, dt); err != nil {
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
	flags.BoolVar(&options.exportLoad, "load", false, `Shorthand for "--set=*.output=type=docker"`)
	flags.BoolVar(&options.printOnly, "print", false, "Print the options without building")
	flags.BoolVar(&options.exportPush, "push", false, `Shorthand for "--set=*.output=type=registry"`)
	flags.StringArrayVar(&options.overrides, "set", nil, `Override target value (e.g., "targetpattern.key=value")`)

	commonBuildFlags(&options.commonOptions, flags)

	return cmd
}
