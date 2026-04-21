package replay

import (
	"encoding/json"

	"github.com/containerd/platforms"
	"github.com/docker/buildx/replay"
	"github.com/docker/buildx/util/buildflags"
	"github.com/docker/buildx/util/cobrautil/completion"
	"github.com/docker/cli/cli"
	"github.com/docker/cli/cli/command"
	"github.com/moby/buildkit/util/progress/progressui"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

// buildOptions holds the parsed flags for `replay build`.
type buildOptions struct {
	commonOptions
	mode       string
	outputs    []string
	tags       []string
	exportLoad bool
	exportPush bool
	dryRun     bool
}

func buildCmd(dockerCli command.Cli, rootOpts RootOptions) *cobra.Command {
	var opts buildOptions

	cmd := &cobra.Command{
		Use:   "build [OPTIONS] SUBJECT",
		Short: "Rebuild an image from provenance and pinned materials",
		Args:  cli.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.builder = *rootOpts.Builder
			return runBuild(cmd, dockerCli, &opts, args[0])
		},
		ValidArgsFunction:     completion.Disable,
		DisableFlagsInUseLine: true,
	}

	installCommonFlags(cmd, &opts.commonOptions)

	flags := cmd.Flags()
	flags.StringVar(&opts.mode, "replay-mode", "materials", `Replay mode ("materials" | "frontend" | "llb")`)
	flags.StringArrayVarP(&opts.outputs, "output", "o", nil, `Output destination (format: "type=local,dest=path")`)
	flags.StringArrayVarP(&opts.tags, "tag", "t", nil, `Image identifier (format: "[registry/]repository[:tag]")`)
	flags.BoolVar(&opts.exportLoad, "load", false, `Shorthand for "--output=type=docker"`)
	flags.BoolVar(&opts.exportPush, "push", false, `Shorthand for "--output=type=registry,unpack=false"`)
	flags.BoolVar(&opts.dryRun, "dry-run", false, "Print a JSON plan of the replay without solving or exporting")

	return cmd
}

// runBuild wires the CLI flags to the replay.Build entry point.
func runBuild(cmd *cobra.Command, dockerCli command.Cli, opts *buildOptions, input string) error {
	ctx := cmd.Context()

	mode := replay.BuildMode(opts.mode)
	switch mode {
	case replay.BuildModeMaterials, replay.BuildModeFrontend:
		// ok
	case replay.BuildModeLLB:
		// Still stubbed in this slice.
		return replay.ErrNotImplemented("llb replay mode")
	default:
		return errors.Errorf("unknown --replay-mode %q", opts.mode)
	}

	// Materials resolver.
	resolver, err := replay.NewMaterialsResolver(opts.materials)
	if err != nil {
		return err
	}

	// Parse flags.
	secretSpecs, err := buildflags.ParseSecretSpecs(opts.secrets)
	if err != nil {
		return errors.Wrap(err, "parse --secret")
	}
	sshSpecs, err := buildflags.ParseSSHSpecs(opts.ssh)
	if err != nil {
		return errors.Wrap(err, "parse --ssh")
	}
	exportSpecs, err := buildflags.ParseExports(opts.outputs)
	if err != nil {
		return errors.Wrap(err, "parse --output")
	}
	exportSpecs = applyExportShorthands(exportSpecs, opts.exportPush, opts.exportLoad)

	// Subject + predicate.
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

	req := &replay.BuildRequest{
		Targets:     targets,
		Mode:        mode,
		Materials:   resolver,
		NetworkMode: opts.network,
		Secrets:     secretSpecs,
		SSH:         sshSpecs,
		Exports:     exportSpecs,
		Tags:        opts.tags,
		Progress:    progressui.DisplayMode(opts.progress),
	}

	if opts.dryRun {
		plan, err := replay.MakeBuildPlan(req)
		if err != nil {
			return err
		}
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(plan)
	}
	return replay.Build(ctx, dockerCli, opts.builder, req)
}

// applyExportShorthands mirrors the --push / --load handling in
// commands/build.go. --push sets push=true (+ unpack=false) on any
// existing type=image export, or appends one; --load appends a
// type=docker export unless an equivalent one is already present.
func applyExportShorthands(exports []*buildflags.ExportEntry, push, load bool) []*buildflags.ExportEntry {
	if push {
		var used bool
		for _, e := range exports {
			if e.Type == "image" {
				if e.Attrs == nil {
					e.Attrs = map[string]string{}
				}
				e.Attrs["push"] = "true"
				if _, ok := e.Attrs["unpack"]; !ok {
					e.Attrs["unpack"] = "false"
				}
				used = true
			}
		}
		if !used {
			exports = append(exports, &buildflags.ExportEntry{
				Type:  "image",
				Attrs: map[string]string{"push": "true", "unpack": "false"},
			})
		}
	}
	if load {
		var used bool
		for _, e := range exports {
			if e.Type == "docker" {
				if _, ok := e.Attrs["dest"]; !ok {
					used = true
					break
				}
			}
		}
		if !used {
			exports = append(exports, &buildflags.ExportEntry{
				Type:  "docker",
				Attrs: map[string]string{},
			})
		}
	}
	return exports
}

// filterSubjectsByPlatform narrows a subject list to the requested platforms.
//
// Contract:
//   - platformFilter == ["all"] keeps every subject.
//   - platformFilter empty defaults to the host's current platform
//     (platforms.DefaultSpec) — replay is single-platform by default.
//   - Otherwise each entry is matched through platforms.Only so that a
//     request for "linux/arm64/v8" accepts a subject tagged "linux/arm64"
//     with an unspecified variant, and vice versa.
//
// An explicit --platform that does not match any subject is an error.
// Subjects with a nil Descriptor.Platform (single-platform images that
// have no per-platform index) are kept unconditionally.
func filterSubjectsByPlatform(subjects []*replay.Subject, platformFilter []string) ([]*replay.Subject, error) {
	if len(platformFilter) == 1 && platformFilter[0] == "all" {
		return subjects, nil
	}
	explicit := len(platformFilter) > 0
	if !explicit {
		platformFilter = []string{platforms.Format(platforms.DefaultSpec())}
	}

	wantNames := make([]string, 0, len(platformFilter))
	matchers := make([]platforms.MatchComparer, 0, len(platformFilter))
	for _, p := range platformFilter {
		pp, err := platforms.Parse(p)
		if err != nil {
			return nil, errors.Wrapf(err, "invalid --platform %q", p)
		}
		matchers = append(matchers, platforms.Only(pp))
		wantNames = append(wantNames, platforms.Format(pp))
	}

	// For each requested platform pick the single best-matching subject —
	// Only() is intentionally permissive (e.g. arm64/v8 matches arm/v5–v7
	// because an arm64 host can run arm32) and we want just the closest
	// platform for the replay.
	matchedAny := make([]bool, len(matchers))
	chosen := map[int]struct{}{}
	for i, m := range matchers {
		best := -1
		for j, s := range subjects {
			if s.Descriptor.Platform == nil {
				continue
			}
			sp := *s.Descriptor.Platform
			if !m.Match(sp) {
				continue
			}
			if best < 0 || m.Less(sp, *subjects[best].Descriptor.Platform) {
				best = j
			}
		}
		if best >= 0 {
			chosen[best] = struct{}{}
			matchedAny[i] = true
		}
	}

	var out []*replay.Subject
	for j, s := range subjects {
		if s.Descriptor.Platform == nil {
			out = append(out, s)
			continue
		}
		if _, ok := chosen[j]; ok {
			out = append(out, s)
		}
	}

	if explicit {
		var missing []string
		for i, w := range wantNames {
			if !matchedAny[i] {
				missing = append(missing, w)
			}
		}
		if len(missing) > 0 {
			return nil, errors.Errorf("requested platform(s) not present: %v", missing)
		}
	}
	if len(out) == 0 {
		return nil, errors.Errorf("no subjects for platform %v — pass --platform <p> or --platform all", wantNames)
	}
	return out, nil
}
