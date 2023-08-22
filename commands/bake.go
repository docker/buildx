package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/containerd/console"
	"github.com/containerd/containerd/platforms"
	"github.com/docker/buildx/bake"
	"github.com/docker/buildx/build"
	"github.com/docker/buildx/builder"
	"github.com/docker/buildx/localstate"
	"github.com/docker/buildx/util/buildflags"
	"github.com/docker/buildx/util/cobrautil/completion"
	"github.com/docker/buildx/util/confutil"
	"github.com/docker/buildx/util/desktop"
	"github.com/docker/buildx/util/dockerutil"
	"github.com/docker/buildx/util/progress"
	"github.com/docker/buildx/util/tracing"
	"github.com/docker/cli/cli/command"
	"github.com/moby/buildkit/identity"
	"github.com/moby/buildkit/util/appcontext"
	"github.com/moby/buildkit/util/progress/progressui"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

type bakeOptions struct {
	files      []string
	overrides  []string
	printOnly  bool
	sbom       string
	provenance string

	builder      string
	metadataFile string
	exportPush   bool
	exportLoad   bool
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
		if build.IsRemoteURL(targets[0]) {
			url = targets[0]
			targets = targets[1:]
			if len(targets) > 0 {
				if build.IsRemoteURL(targets[0]) {
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
	if cFlags.noCache != nil {
		overrides = append(overrides, fmt.Sprintf("*.no-cache=%t", *cFlags.noCache))
	}
	if cFlags.pull != nil {
		overrides = append(overrides, fmt.Sprintf("*.pull=%t", *cFlags.pull))
	}
	if in.sbom != "" {
		overrides = append(overrides, fmt.Sprintf("*.attest=%s", buildflags.CanonicalizeAttest("sbom", in.sbom)))
	}
	if in.provenance != "" {
		overrides = append(overrides, fmt.Sprintf("*.attest=%s", buildflags.CanonicalizeAttest("provenance", in.provenance)))
	}
	contextPathHash, _ := os.Getwd()

	ctx2, cancel := context.WithCancel(context.TODO())
	defer cancel()

	var nodes []builder.Node
	var files []bake.File
	var inp *bake.Input
	var progressConsoleDesc, progressTextDesc string

	// instance only needed for reading remote bake files or building
	if url != "" || !in.printOnly {
		b, err := builder.New(dockerCli,
			builder.WithName(in.builder),
			builder.WithContextPathHash(contextPathHash),
		)
		if err != nil {
			return err
		}
		if err = updateLastActivity(dockerCli, b.NodeGroup); err != nil {
			return errors.Wrapf(err, "failed to update builder last activity time")
		}
		nodes, err = b.LoadNodes(ctx)
		if err != nil {
			return err
		}
		progressConsoleDesc = fmt.Sprintf("%s:%s", b.Driver, b.Name)
		progressTextDesc = fmt.Sprintf("building with %q instance using %s driver", b.Name, b.Driver)
	}

	var term bool
	if _, err := console.ConsoleFromFile(os.Stderr); err == nil {
		term = true
	}

	progressMode := progressui.DisplayMode(cFlags.progress)
	printer, err := progress.NewPrinter(ctx2, os.Stderr, progressMode,
		progress.WithDesc(progressTextDesc, progressConsoleDesc),
	)
	if err != nil {
		return err
	}

	defer func() {
		if printer != nil {
			err1 := printer.Wait()
			if err == nil {
				err = err1
			}
			if err == nil && progressMode != progressui.QuietMode {
				desktop.PrintBuildDetails(os.Stderr, printer.BuildRefs(), term)
			}
		}
	}()

	if url != "" {
		files, inp, err = bake.ReadRemoteFiles(ctx, nodes, url, in.files, printer)
	} else {
		files, err = bake.ReadLocalFiles(in.files, dockerCli.In())
	}
	if err != nil {
		return err
	}

	tgts, grps, err := bake.ReadTargets(ctx, files, targets, overrides, map[string]string{
		// don't forget to update documentation if you add a new
		// built-in variable: docs/bake-reference.md#built-in-variables
		"BAKE_CMD_CONTEXT":    cmdContext,
		"BAKE_LOCAL_PLATFORM": platforms.DefaultString(),
	})
	if err != nil {
		return err
	}

	if v := os.Getenv("SOURCE_DATE_EPOCH"); v != "" {
		// TODO: extract env var parsing to a method easily usable by library consumers
		for _, t := range tgts {
			if _, ok := t.Args["SOURCE_DATE_EPOCH"]; ok {
				continue
			}
			if t.Args == nil {
				t.Args = map[string]*string{}
			}
			t.Args["SOURCE_DATE_EPOCH"] = &v
		}
	}

	// this function can update target context string from the input so call before printOnly check
	bo, err := bake.TargetsToBuildOpt(tgts, inp)
	if err != nil {
		return err
	}

	def := struct {
		Group  map[string]*bake.Group  `json:"group,omitempty"`
		Target map[string]*bake.Target `json:"target"`
	}{
		Group:  grps,
		Target: tgts,
	}

	if in.printOnly {
		dt, err := json.MarshalIndent(def, "", "  ")
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

	// local state group
	groupRef := identity.NewID()
	var refs []string
	for k, b := range bo {
		b.Ref = identity.NewID()
		b.GroupRef = groupRef
		refs = append(refs, b.Ref)
		bo[k] = b
	}
	dt, err := json.Marshal(def)
	if err != nil {
		return err
	}
	if err := saveLocalStateGroup(dockerCli, groupRef, localstate.StateGroup{
		Definition: dt,
		Targets:    targets,
		Inputs:     overrides,
		Refs:       refs,
	}); err != nil {
		return err
	}

	resp, err := build.Build(ctx, nodes, bo, dockerutil.NewClient(dockerCli), confutil.ConfigDir(dockerCli), printer)
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
			options.builder = rootOpts.builder
			options.metadataFile = cFlags.metadataFile
			// Other common flags (noCache, pull and progress) are processed in runBake function.
			return runBake(dockerCli, args, options, cFlags)
		},
		ValidArgsFunction: completion.BakeTargets(options.files),
	}

	flags := cmd.Flags()

	flags.StringArrayVarP(&options.files, "file", "f", []string{}, "Build definition file")
	flags.BoolVar(&options.exportLoad, "load", false, `Shorthand for "--set=*.output=type=docker"`)
	flags.BoolVar(&options.printOnly, "print", false, "Print the options without building")
	flags.BoolVar(&options.exportPush, "push", false, `Shorthand for "--set=*.output=type=registry"`)
	flags.StringVar(&options.sbom, "sbom", "", `Shorthand for "--set=*.attest=type=sbom"`)
	flags.StringVar(&options.provenance, "provenance", "", `Shorthand for "--set=*.attest=type=provenance"`)
	flags.StringArrayVar(&options.overrides, "set", nil, `Override target value (e.g., "targetpattern.key=value")`)

	commonBuildFlags(&cFlags, flags)

	return cmd
}

func saveLocalStateGroup(dockerCli command.Cli, ref string, lsg localstate.StateGroup) error {
	l, err := localstate.New(confutil.ConfigDir(dockerCli))
	if err != nil {
		return err
	}
	return l.SaveGroup(ref, lsg)
}
