package replay

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/containerd/platforms"
	"github.com/docker/buildx/replay"
	"github.com/docker/buildx/util/buildflags"
	"github.com/docker/buildx/util/cobrautil/completion"
	"github.com/docker/buildx/util/platformutil"
	"github.com/docker/cli/cli"
	"github.com/docker/cli/cli/command"
	"github.com/moby/buildkit/util/progress/progressui"
	ocispecs "github.com/opencontainers/image-spec/specs-go/v1"
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
	format     string
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
	flags.BoolVar(&opts.dryRun, "dry-run", false, "Print a plan of the replay without solving or exporting")
	flags.StringVar(&opts.format, "format", "pretty", `Format dry-run output ("pretty" | "json")`)

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
	if err := replay.VerifySignatures(ctx, dockerCli, subjects); err != nil {
		return err
	}

	targets := make([]replay.Target, 0, len(subjects))
	for _, s := range subjects {
		pred, err := s.Predicate(ctx)
		if err != nil {
			return err
		}
		s = applyPredicateTargetPlatformFallback(s, pred, opts.platforms)
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
		switch opts.format {
		case "pretty":
			return printBuildPlan(cmd.OutOrStdout(), plan)
		case "json":
			enc := json.NewEncoder(cmd.OutOrStdout())
			enc.SetIndent("", "  ")
			return enc.Encode(plan)
		default:
			return errors.Errorf("unknown --format %q", opts.format)
		}
	}
	return replay.Build(ctx, dockerCli, opts.builder, req)
}

func printBuildPlan(out io.Writer, plan *replay.BuildPlan) error {
	if plan == nil {
		return errors.New("nil build plan")
	}
	tw := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	if _, err := fmt.Fprintln(tw, "Replay plan"); err != nil {
		return errors.WithStack(err)
	}
	for i, subject := range plan.Subjects {
		if _, err := fmt.Fprintf(tw, "\nSubject %d/%d\n", i+1, len(plan.Subjects)); err != nil {
			return errors.WithStack(err)
		}
		if err := writePlanField(tw, "Platform", formatPlanPlatform(subject.Descriptor.Platform)); err != nil {
			return err
		}
		if err := writePlanField(tw, "Digest", subject.Descriptor.Digest.String()); err != nil {
			return err
		}
		if _, err := fmt.Fprintln(tw, "\nBuild configuration"); err != nil {
			return errors.WithStack(err)
		}
		cfg := subject.BuildConfig
		for _, field := range []struct{ name, value string }{
			{"Frontend", cfg.Frontend},
			{"Context", cfg.Context},
			{"Dockerfile", cfg.Filename},
			{"Target", cfg.Target},
			{"Network", cfg.NetworkMode},
		} {
			if err := writePlanField(tw, field.name, field.value); err != nil {
				return err
			}
		}
		if err := writePlanMap(tw, "Build args", cfg.BuildArgs); err != nil {
			return err
		}
		if len(cfg.Secrets) > 0 {
			values := make([]string, 0, len(cfg.Secrets))
			for _, secret := range cfg.Secrets {
				value := secret.ID
				if secret.Optional {
					value += " (optional)"
				}
				values = append(values, value)
			}
			if err := writePlanField(tw, "Secrets", fmt.Sprintf("%v", values)); err != nil {
				return err
			}
		}
		if err := writePlanSignature(tw, subject.Signature); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(tw, "\nMaterials (%d)\n", len(subject.Materials)); err != nil {
			return errors.WithStack(err)
		}
		for _, material := range subject.Materials {
			platform := formatPlanPlatform(material.Platform)
			if platform != "" {
				platform = " [" + platform + "]"
			}
			if _, err := fmt.Fprintf(tw, "  %s%s\t%s\n", material.Kind, platform, material.URI); err != nil {
				return errors.WithStack(err)
			}
			if material.Digest != "" {
				if _, err := fmt.Fprintf(tw, "  \t%s\n", material.Digest); err != nil {
					return errors.WithStack(err)
				}
			}
		}
	}
	return errors.WithStack(tw.Flush())
}

func writePlanSignature(w io.Writer, signature *replay.SignatureVerification) error {
	if signature == nil {
		return nil
	}
	if _, err := fmt.Fprintf(w, "\n%s\n", signature.Type); err != nil {
		return errors.WithStack(err)
	}
	for _, field := range []struct{ name, value string }{
		{"Verified signer", signature.Identity},
		{"Signer identity", signature.SubjectAlternativeName},
		{"Certificate issuer", signature.CertificateIssuer},
		{"OIDC issuer", signature.Issuer},
		{"Runner environment", signature.RunnerEnvironment},
		{"Source repository", signature.SourceRepositoryURI},
		{"Source ref", signature.SourceRepositoryRef},
	} {
		if err := writePlanField(w, field.name, field.value); err != nil {
			return err
		}
	}
	if signature.BuildSignerURI != signature.SubjectAlternativeName {
		if err := writePlanField(w, "Build signer", signature.BuildSignerURI); err != nil {
			return err
		}
	}
	if len(signature.Timestamps) > 0 {
		if _, err := fmt.Fprintln(w, "    TYPE\tTIME\tSOURCE"); err != nil {
			return errors.WithStack(err)
		}
	}
	for _, timestamp := range signature.Timestamps {
		typeName := timestamp.Type
		switch timestamp.Type {
		case "Tlog":
			typeName = "Transparency log"
		case "TimestampAuthority":
			typeName = "Timestamp authority"
		}
		value := timestamp.Timestamp.Format(time.RFC3339)
		if _, err := fmt.Fprintf(w, "    %s\t%s\t%s\n", typeName, value, timestamp.URI); err != nil {
			return errors.WithStack(err)
		}
	}
	if signature.TrustRootWarning != "" {
		if err := writePlanField(w, "Trust root warning", signature.TrustRootWarning); err != nil {
			return err
		}
	}
	return nil
}

func writePlanField(w io.Writer, name, value string) error {
	if value == "" {
		return nil
	}
	_, err := fmt.Fprintf(w, "  %s:\t%s\n", name, value)
	return errors.WithStack(err)
}

func writePlanMap(w io.Writer, name string, values map[string]string) error {
	if len(values) == 0 {
		return nil
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for i, key := range keys {
		label := ""
		if i == 0 {
			label = name + ":"
		}
		if _, err := fmt.Fprintf(w, "  %s\t%s=%s\n", label, key, values[key]); err != nil {
			return errors.WithStack(err)
		}
	}
	return nil
}

func formatPlanPlatform(platform *ocispecs.Platform) string {
	if platform == nil {
		return ""
	}
	return platforms.Format(*platform)
}

// applyPredicateTargetPlatformFallback fills the platform metadata that a raw
// provenance file cannot carry on its subject descriptor. Image subjects get
// this metadata from their manifest index; attestation files can fall back to
// TARGETPLATFORM inferred from the recorded LLB. An explicit --platform
// has already been applied by filterSubjectsByPlatform and takes precedence.
func applyPredicateTargetPlatformFallback(subject *replay.Subject, pred *replay.Predicate, platformFilter []string) *replay.Subject {
	if subject == nil || pred == nil || subject.Descriptor.Platform != nil || len(platformFilter) != 0 {
		return subject
	}
	platform, ok := pred.FallbackTargetPlatform()
	if !ok {
		return subject
	}
	clone := *subject
	clone.Descriptor = subject.Descriptor
	p := *platform
	clone.Descriptor.Platform = &p
	return &clone
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
//   - Comma-separated and repeated entries are equivalent.
//   - platformFilter empty defaults to the host's current platform
//     (platforms.DefaultSpec) — replay is single-platform by default.
//   - Otherwise each entry is matched strictly after normalization. Platform
//     selection identifies an artifact; it is not an execution-compatibility
//     check.
//
// An explicit --platform that does not match any subject is an error.
// A sole subject with no descriptor platform inherits each explicit platform
// because there is no index metadata to select from.
func filterSubjectsByPlatform(subjects []*replay.Subject, platformFilter []string) ([]*replay.Subject, error) {
	explicit := len(platformFilter) > 0
	wantPlatforms, all, err := parsePlatformFilter(platformFilter)
	if err != nil {
		return nil, err
	}
	if all {
		return subjects, nil
	}
	if !explicit {
		wantPlatforms = []ocispecs.Platform{platforms.Normalize(platforms.DefaultSpec())}
	}

	wantNames := make([]string, 0, len(wantPlatforms))
	matchers := make([]platforms.MatchComparer, 0, len(wantPlatforms))
	for _, platform := range wantPlatforms {
		matchers = append(matchers, platforms.OnlyStrict(platform))
		wantNames = append(wantNames, platforms.Format(platform))
	}
	if explicit && len(subjects) == 1 && subjects[0].Descriptor.Platform == nil {
		out := make([]*replay.Subject, 0, len(wantPlatforms))
		for _, platform := range wantPlatforms {
			subject := *subjects[0]
			subject.Descriptor = subjects[0].Descriptor
			p := platform
			subject.Descriptor.Platform = &p
			out = append(out, &subject)
		}
		return out, nil
	}

	// For each requested platform pick the single best-matching subject —
	// duplicate descriptors are collapsed to one target.
	matchedAny := make([]bool, len(matchers))
	chosen := make([]int, 0, len(matchers))
	chosenSet := map[int]struct{}{}
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
			if _, exists := chosenSet[best]; !exists {
				chosenSet[best] = struct{}{}
				chosen = append(chosen, best)
			}
			matchedAny[i] = true
		}
	}

	out := make([]*replay.Subject, 0, len(chosen))
	for _, j := range chosen {
		out = append(out, subjects[j])
	}
	for _, s := range subjects {
		if s.Descriptor.Platform == nil {
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

func parsePlatformFilter(values []string) ([]ocispecs.Platform, bool, error) {
	var flattened []string
	for _, value := range values {
		for part := range strings.SplitSeq(value, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				return nil, false, errors.New("invalid empty --platform value")
			}
			flattened = append(flattened, part)
		}
	}
	for _, value := range flattened {
		if value != "all" {
			continue
		}
		if len(flattened) != 1 {
			return nil, false, errors.New(`--platform "all" cannot be combined with other platforms`)
		}
		return nil, true, nil
	}
	parsed, err := platformutil.Parse(flattened)
	if err != nil {
		return nil, false, errors.Wrap(err, "invalid --platform")
	}
	return platformutil.Dedupe(parsed), false, nil
}
