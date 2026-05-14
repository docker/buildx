package replay

import (
	"encoding/json"
	"os"

	"github.com/docker/buildx/replay"
	"github.com/docker/buildx/util/buildflags"
	"github.com/docker/buildx/util/cobrautil/completion"
	"github.com/docker/buildx/util/progress"
	"github.com/docker/cli/cli"
	"github.com/docker/cli/cli/command"
	"github.com/moby/buildkit/util/progress/progressui"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// snapshotOptions holds the parsed flags for `replay snapshot`.
type snapshotOptions struct {
	commonOptions
	includeMaterials bool
	outputs          []string
	dryRun           bool
}

func snapshotCmd(dockerCli command.Cli, rootOpts RootOptions) *cobra.Command {
	var opts snapshotOptions

	cmd := &cobra.Command{
		Use:   "snapshot [OPTIONS] SUBJECT",
		Short: "Export replay inputs for a subject as a reusable materials store",
		Args:  cli.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.builder = *rootOpts.Builder
			return runSnapshot(cmd, dockerCli, &opts, args[0])
		},
		ValidArgsFunction:     completion.Disable,
		DisableFlagsInUseLine: true,
	}

	installCommonFlags(cmd, &opts.commonOptions)

	flags := cmd.Flags()
	flags.BoolVar(&opts.includeMaterials, "include-materials", true, "Include material content in the snapshot")
	flags.StringArrayVarP(&opts.outputs, "output", "o", nil, `Output destination (default: "-" — oci tar to stdout; bare "<path>" writes an oci-layout directory; "type=oci,dest=X[,tar=true|false]"; "type=registry,name=<ref>")`)
	flags.BoolVar(&opts.dryRun, "dry-run", false, "Print a JSON plan of the snapshot without writing output")

	return cmd
}

// runSnapshot wires the CLI flags to the replay.Snapshot entry point.
func runSnapshot(cmd *cobra.Command, dockerCli command.Cli, opts *snapshotOptions, input string) error {
	ctx := cmd.Context()

	// Resolve --output → a normalized snapshot export spec. Dry-run does not
	// write anything so we skip the TTY refusal and terminal checks there.
	var exportSpec *buildflags.ExportEntry
	if !opts.dryRun {
		spec, err := resolveSnapshotOutput(opts.outputs)
		if err != nil {
			return err
		}
		exportSpec = spec
	}

	// Materials resolver — used to lookup pre-pinned content when
	// --materials is supplied.
	resolver, err := replay.NewMaterialsResolver(opts.materials)
	if err != nil {
		return err
	}

	subjects, err := replay.LoadSubjects(ctx, dockerCli, opts.builder, input)
	if err != nil {
		return err
	}

	subjects, err = filterSubjectsByPlatform(subjects, opts.platforms)
	if err != nil {
		return err
	}
	if len(subjects) == 0 {
		return errors.New("no subjects matched the --platform filter")
	}

	targets := make([]replay.Target, 0, len(subjects))
	for _, s := range subjects {
		pred, err := s.Predicate(ctx)
		if err != nil {
			return err
		}
		targets = append(targets, replay.Target{Subject: s, Predicate: pred})
	}

	req := &replay.SnapshotRequest{
		Targets:          targets,
		IncludeMaterials: opts.includeMaterials,
		Materials:        resolver,
		Output:           exportSpec,
	}

	// Both real-run and dry-run do the same staging work (dry-run just
	// skips the final output), so both get a progress printer.
	printer, err := progress.NewPrinter(ctx, os.Stderr, progressui.DisplayMode(opts.progress))
	if err != nil {
		return err
	}
	req.Progress = printer

	if opts.dryRun {
		plan, planErr := replay.MakeSnapshotPlan(ctx, dockerCli, opts.builder, req)
		// Wait for the progress printer to drain before writing the JSON
		// plan: in auto/tty mode the printer owns the terminal and its
		// final redraw otherwise interleaves with stdout.
		waitErr := printer.Wait()
		if planErr != nil {
			return planErr
		}
		if waitErr != nil {
			return waitErr
		}
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(plan)
	}

	snapErr := replay.Snapshot(ctx, dockerCli, opts.builder, req)
	if waitErr := printer.Wait(); snapErr == nil {
		snapErr = waitErr
	}
	return snapErr
}

// resolveSnapshotOutput turns raw --output values into a normalized
// ExportEntry with Type ∈ {"oci", "registry"}. The command surface is:
//
//	(unset)                            → type=oci, dest=-       (stdout tar)
//	-o -                               → type=oci, dest=-       (stdout tar)
//	-o <path>                          → type=oci, dest=<path>, tar=false (layout dir)
//	-o type=oci,dest=<file>[,tar=...]  → oci, defaults to tar=true
//	-o type=registry,name=<ref>        → registry push
//
// A TTY on stdout with no --output (or -o -) is refused: writing a
// multi-megabyte binary tar to a terminal is never what the user wants.
func resolveSnapshotOutput(outputs []string) (*buildflags.ExportEntry, error) {
	if len(outputs) > 1 {
		return nil, errors.Errorf("snapshot: exactly one --output is required (got %d)", len(outputs))
	}

	var out buildflags.ExportEntry
	if len(outputs) == 0 {
		out = buildflags.ExportEntry{Type: "oci", Destination: "-"}
	} else {
		parsed, err := buildflags.ParseExports(outputs)
		if err != nil {
			return nil, errors.Wrap(err, "parse --output")
		}
		if len(parsed) != 1 {
			return nil, errors.Errorf("snapshot: exactly one --output is required (got %d)", len(parsed))
		}
		out = *parsed[0]
	}

	// buildflags.ParseExports maps a bare "-" to type="tar" and a bare
	// "<path>" to type="local". Translate both into our oci surface.
	switch out.Type {
	case "tar":
		out.Type = "oci"
	case "local":
		// Bare path → oci-layout directory.
		out.Type = "oci"
		if out.Attrs == nil {
			out.Attrs = map[string]string{}
		}
		out.Attrs["tar"] = "false"
	}

	if out.Type == "oci" && out.Destination == "-" {
		if term.IsTerminal(int(os.Stdout.Fd())) {
			return nil, errors.New("refusing to write binary snapshot to terminal — set an --output file or directory")
		}
	}

	if out.Type != "oci" && out.Type != "registry" {
		return nil, errors.Errorf("snapshot: unsupported --output type %q (want oci | registry)", out.Type)
	}
	return &out, nil
}
