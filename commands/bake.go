package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/containerd/containerd/platforms"
	"github.com/docker/buildx/bake"
	"github.com/docker/buildx/build"
	"github.com/docker/buildx/builder"
	controllerapi "github.com/docker/buildx/commands/controller/pb"
	"github.com/docker/buildx/util/buildflags"
	"github.com/docker/buildx/util/confutil"
	"github.com/docker/buildx/util/dockerutil"
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
	controllerapi.CommonOptions
}

func runBake(dockerCli command.Cli, targets []string, in bakeOptions, cFlags commonFlags) (err error) {
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
	if in.ExportPush {
		if in.ExportLoad {
			return errors.Errorf("push and load may not be set together at the moment")
		}
		overrides = append(overrides, "*.push=true")
	} else if in.ExportLoad {
		overrides = append(overrides, "*.output=type=docker")
	}
	if cFlags.noCache != nil {
		overrides = append(overrides, fmt.Sprintf("*.no-cache=%t", *cFlags.noCache))
	}
	if cFlags.pull != nil {
		overrides = append(overrides, fmt.Sprintf("*.pull=%t", *cFlags.pull))
	}
	if in.SBOM != "" {
		overrides = append(overrides, fmt.Sprintf("*.attest=%s", buildflags.CanonicalizeAttest("sbom", in.SBOM)))
	}
	if in.Provenance != "" {
		overrides = append(overrides, fmt.Sprintf("*.attest=%s", buildflags.CanonicalizeAttest("provenance", in.Provenance)))
	}
	contextPathHash, _ := os.Getwd()

	ctx2, cancel := context.WithCancel(context.TODO())
	defer cancel()
	printer, err := progress.NewPrinter(ctx2, os.Stderr, os.Stderr, cFlags.progress)
	if err != nil {
		return err
	}

	defer func() {
		if printer != nil {
			err1 := printer.Wait()
			if err == nil {
				err = err1
			}
		}
	}()

	var nodes []builder.Node
	var files []bake.File
	var inp *bake.Input

	// instance only needed for reading remote bake files or building
	if url != "" || !in.printOnly {
		b, err := builder.New(dockerCli,
			builder.WithName(in.Builder),
			builder.WithContextPathHash(contextPathHash),
		)
		if err != nil {
			return err
		}
		if err = updateLastActivity(dockerCli, b.NodeGroup); err != nil {
			return errors.Wrapf(err, "failed to update builder last activity time")
		}
		nodes, err = b.LoadNodes(ctx, false)
		if err != nil {
			return err
		}
	}

	if url != "" {
		files, inp, err = bake.ReadRemoteFiles(ctx, nodes, url, in.files, printer)
	} else {
		files, err = bake.ReadLocalFiles(in.files)
	}
	if err != nil {
		return err
	}

	tgts, grps, err := bake.ReadTargets(ctx, files, targets, overrides, map[string]string{
		// don't forget to update documentation if you add a new
		// built-in variable: docs/manuals/bake/file-definition.md#built-in-variables
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
		dt, err := json.MarshalIndent(struct {
			Group  map[string]*bake.Group  `json:"group,omitempty"`
			Target map[string]*bake.Target `json:"target"`
		}{
			grps,
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

	resp, err := build.Build(ctx, nodes, bo, dockerutil.NewClient(dockerCli), confutil.ConfigDir(dockerCli), printer)
	if err != nil {
		return wrapBuildError(err, true)
	}

	if len(in.MetadataFile) > 0 {
		dt := make(map[string]interface{})
		for t, r := range resp {
			dt[t] = decodeExporterResponse(r.ExporterResponse)
		}
		if err := writeMetadataFile(in.MetadataFile, dt); err != nil {
			return err
		}
	}

	return err
}

func bakeCmd(dockerCli command.Cli, rootOpts *rootOptions) *cobra.Command {
	var options bakeOptions
	var cFlags commonFlags

	cmd := &cobra.Command{
		Use:     "bake [OPTIONS] [TARGET...]",
		Aliases: []string{"f"},
		Short:   "Build from a file",
		RunE: func(cmd *cobra.Command, args []string) error {
			// reset to nil to avoid override is unset
			if !cmd.Flags().Lookup("no-cache").Changed {
				cFlags.noCache = nil
			}
			if !cmd.Flags().Lookup("pull").Changed {
				cFlags.pull = nil
			}
			options.Builder = rootOpts.builder
			options.MetadataFile = cFlags.metadataFile
			// Other common flags (noCache, pull and progress) are processed in runBake function.
			return runBake(dockerCli, args, options, cFlags)
		},
	}

	flags := cmd.Flags()

	flags.StringArrayVarP(&options.files, "file", "f", []string{}, "Build definition file")
	flags.BoolVar(&options.ExportLoad, "load", false, `Shorthand for "--set=*.output=type=docker"`)
	flags.BoolVar(&options.printOnly, "print", false, "Print the options without building")
	flags.BoolVar(&options.ExportPush, "push", false, `Shorthand for "--set=*.output=type=registry"`)
	flags.StringVar(&options.SBOM, "sbom", "", `Shorthand for "--set=*.attest=type=sbom"`)
	flags.StringVar(&options.Provenance, "provenance", "", `Shorthand for "--set=*.attest=type=provenance"`)
	flags.StringArrayVar(&options.overrides, "set", nil, `Override target value (e.g., "targetpattern.key=value")`)

	commonBuildFlags(&cFlags, flags)

	return cmd
}
