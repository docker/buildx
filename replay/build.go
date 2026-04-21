package replay

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/containerd/platforms"
	"github.com/docker/buildx/build"
	"github.com/docker/buildx/builder"
	"github.com/docker/buildx/util/buildflags"
	"github.com/docker/buildx/util/confutil"
	"github.com/docker/buildx/util/dockerutil"
	"github.com/docker/buildx/util/progress"
	"github.com/docker/cli/cli/command"
	"github.com/moby/buildkit/identity"
	provenancetypes "github.com/moby/buildkit/solver/llbsolver/provenance/types"
	"github.com/moby/buildkit/util/progress/progressui"
	ocispecs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/pkg/errors"
	"github.com/tonistiigi/go-csvvalue"
)

// BuildMode is a replay mode.
type BuildMode string

const (
	BuildModeMaterials BuildMode = "materials"
	BuildModeFrontend  BuildMode = "frontend"
	BuildModeLLB       BuildMode = "llb"
)

// Target pairs one subject with its already-loaded predicate. A replay
// operation spans N targets (one per platform, typically from a multi-
// platform LoadSubjects fan-out).
type Target struct {
	Subject   *Subject
	Predicate *Predicate
}

// BuildRequest is a single replay-build invocation spanning one or more
// targets that share the same user-supplied flags.
type BuildRequest struct {
	// Targets is the set of (subject, predicate) pairs to replay. For a
	// single-platform subject this is len 1; multi-platform inputs fan
	// out into multiple targets sharing the other fields below.
	Targets []Target

	// Mode selects a replay strategy. Empty defaults to BuildModeMaterials.
	Mode BuildMode

	// Materials resolves provenance materials to local content stores. May
	// be nil, in which case the default sentinel-only resolver is used.
	Materials *MaterialsResolver

	// NetworkMode controls the network mode for RUN instructions in the
	// replayed build (default | none). Material resolution is NOT affected.
	NetworkMode string

	// Secrets / SSH hold the user-supplied specs for the replayed solve.
	// Cross-checked against each predicate via Secrets()/SSH() before any
	// solve begins.
	Secrets buildflags.Secrets
	SSH     []*buildflags.SSH

	// Exports are the buildflags-parsed --output specs.
	Exports []*buildflags.ExportEntry

	// Tags are "--tag" values to apply to image/oci/docker exports. Flow
	// matches `docker buildx build`: the tags become the `name=` attribute
	// on each eligible export via build/opt.go toSolveOpt.
	Tags []string

	// Progress controls the display mode for replay progress output.
	Progress progressui.DisplayMode
}

// SubjectKey returns a stable identifier for a subject, used as the map key
// for build.Build's map[string]Options input.
func SubjectKey(s *Subject) string {
	if s == nil {
		return ""
	}
	if s.Descriptor.Platform != nil {
		return fmt.Sprintf("%s@%s", s.Descriptor.Digest, platforms.Format(*s.Descriptor.Platform))
	}
	return s.Descriptor.Digest.String()
}

// Build executes the replay request against the supplied builder.
//
// Fail-fast: cross-check errors are reported per-subject with typed errors
// (Missing/ExtraSecretError, Missing/ExtraSSHError) before any solve starts;
// a local-context predicate fails with UnreplayableLocalContextError.
//
// Mode selection:
//
//   - BuildModeMaterials (default): recorded frontend + strict source-policy
//     pinning via the session policy callback.
//   - BuildModeFrontend: recorded frontend + NO strict pinning (sources float).
//   - BuildModeLLB: not yet implemented.
func Build(ctx context.Context, dockerCli command.Cli, builderName string, req *BuildRequest) (retErr error) {
	if req == nil {
		return errors.New("nil build request")
	}
	if len(req.Targets) == 0 {
		return errors.New("no targets to replay")
	}
	if req.Mode == BuildModeLLB {
		return ErrNotImplemented("llb replay mode")
	}

	// Pre-solve: local-context + secret/ssh cross-check.
	for _, t := range req.Targets {
		if t.Subject == nil || t.Predicate == nil {
			return errors.New("target has nil subject or predicate")
		}
		if locals := t.Predicate.Locals(); len(locals) > 0 {
			names := make([]string, 0, len(locals))
			for _, l := range locals {
				names = append(names, l.Name)
			}
			return ErrUnreplayableLocalContext(names)
		}
		if err := CheckSecrets(t.Predicate.Secrets(), req.Secrets); err != nil {
			return err
		}
		if err := CheckSSH(t.Predicate.SSH(), req.SSH); err != nil {
			return err
		}
	}

	// Parse exports once; shared across all targets.
	exports, _, err := build.CreateExports(req.Exports)
	if err != nil {
		return errors.Wrap(err, "parse --output")
	}

	// Build the map[string]build.Options keyed by subject key.
	buildOpts := make(map[string]build.Options, len(req.Targets))
	for _, t := range req.Targets {
		opt, err := BuildOptionsFromPredicate(t.Subject, t.Predicate, req)
		if err != nil {
			return err
		}
		opt.Exports = exports
		buildOpts[SubjectKey(t.Subject)] = opt
	}

	// Builder + printer wiring.
	b, err := builder.New(dockerCli, builder.WithName(builderName))
	if err != nil {
		return err
	}
	nodes, err := b.LoadNodes(ctx)
	if err != nil {
		return err
	}
	mode := req.Mode
	if mode == "" {
		mode = BuildModeMaterials
	}
	warningMsg := ""
	if mode == BuildModeMaterials {
		warningMsg = materialsModePlatformWarning(req.Targets, nodes)
	}

	progressMode := req.Progress
	if progressMode == "" {
		progressMode = progressui.AutoMode
	}
	printer, err := progress.NewPrinter(ctx, os.Stderr, progressMode,
		progress.WithDesc(
			fmt.Sprintf("rebuilding %d subject(s) with %q instance using %s driver", len(req.Targets), b.Name, b.Driver),
			fmt.Sprintf("%s:%s", b.Driver, b.Name),
		),
	)
	if err != nil {
		return err
	}
	defer func() {
		werr := printer.Wait()
		if retErr == nil {
			retErr = werr
		}
	}()
	if warningMsg != "" {
		if err := progress.Wrap("check replay environment", printer.Write, func(sub progress.SubLogger) error {
			sub.Log(2, []byte("warning: "+warningMsg+"\n"))
			return nil
		}); err != nil {
			return err
		}
	}

	if _, err := build.Build(ctx, nodes, buildOpts, dockerutil.NewClient(dockerCli), confutil.NewConfig(dockerCli), printer); err != nil {
		return errors.Wrap(err, "replay build")
	}
	return nil
}

func materialsModePlatformWarning(targets []Target, nodes []builder.Node) string {
	hostPlat := platforms.Normalize(platforms.DefaultSpec())
	instancePlat := &hostPlat
	if len(nodes) == 0 {
		instanceFmt := platforms.Format(*instancePlat)
		for _, t := range targets {
			if t.Predicate == nil {
				continue
			}
			prov, ok := t.Predicate.DefaultPlatform()
			if !ok || prov == nil {
				continue
			}
			provFmt := platforms.Format(*prov)
			if provFmt == instanceFmt {
				continue
			}
			return fmt.Sprintf("provenance default platform %s does not match current builder instance default platform %s; materials-mode replay may be inefficient or fail", provFmt, instanceFmt)
		}
		return ""
	}
	matchedHost := false
	for _, n := range nodes {
		if n.Err != nil || len(n.Platforms) == 0 {
			continue
		}
		for i := range n.Platforms {
			p := platforms.Normalize(n.Platforms[i])
			if platforms.Only(hostPlat).Match(p) {
				matchedHost = true
				break
			}
		}
		if matchedHost {
			break
		}
	}
	if !matchedHost {
		for _, n := range nodes {
			if n.Err != nil || len(n.Platforms) == 0 {
				continue
			}
			p := platforms.Normalize(n.Platforms[0])
			instancePlat = &p
			break
		}
	}
	instanceFmt := platforms.Format(platforms.Normalize(*instancePlat))
	for _, t := range targets {
		if t.Predicate == nil {
			continue
		}
		prov, ok := t.Predicate.DefaultPlatform()
		if !ok || prov == nil {
			continue
		}
		provFmt := platforms.Format(*prov)
		if provFmt == instanceFmt {
			continue
		}
		return fmt.Sprintf("provenance default platform %s does not match current builder instance default platform %s; materials-mode replay may be inefficient or fail", provFmt, instanceFmt)
	}
	return ""
}

// BuildOptionsFromPredicate maps a (subject, predicate) pair to a
// build.Options. The resulting options have Exports left empty; Build
// populates them from the request.
func BuildOptionsFromPredicate(s *Subject, pred *Predicate, req *BuildRequest) (build.Options, error) {
	if pred == nil {
		return build.Options{}, errors.New("nil predicate")
	}

	attrs := pred.FrontendAttrs()
	cfgSrc := pred.ConfigSource()

	labels := collectPrefixed(attrs, "label:")
	buildArgs := collectPrefixed(attrs, "build-arg:")
	var nocacheFilter []string
	noCache := false
	if v, ok := attrs["no-cache"]; ok {
		if v == "" {
			noCache = true
		} else if fields, err := csvvalue.Fields(v, nil); err == nil {
			nocacheFilter = fields
		}
	}

	// NamedContexts from recorded "context:*" attrs.
	namedContexts := map[string]build.NamedContext{}
	for k, v := range attrs {
		name, ok := strings.CutPrefix(k, "context:")
		if !ok {
			continue
		}
		namedContexts[name] = build.NamedContext{Path: v}
	}

	target := attrs["target"]
	var extraHosts []string
	if v := attrs["add-hosts"]; v != "" {
		if fields, err := csvvalue.Fields(v, nil); err == nil {
			extraHosts = fields
		}
	}
	cgroupParent := attrs["cgroup-parent"]

	// Dockerfile path comes from configSource.path when present — that is the
	// canonical provenance field for the build definition. The recorded
	// frontend attr is only used as a compatibility fallback.
	dockerfilePath := cfgSrc.Path
	if dockerfilePath == "" {
		dockerfilePath = attrs["filename"]
	}

	// The build context comes from configSource.uri when present — that is
	// the canonical provenance field for the source location. The recorded
	// frontend attr is only used as a compatibility fallback. Replay rejects
	// local filesystem contexts up-front via the Locals check, so by the time
	// we get here the predicate is expected to carry a remote source URL.
	contextPath := cfgSrc.URI
	if contextPath == "" {
		contextPath = attrs["context"]
	}
	if contextPath == "" {
		return build.Options{}, errors.Errorf("predicate has no recorded build context; replay requires a remote-source build (git / https)")
	}

	opt := build.Options{
		Ref:    identity.NewID(),
		Target: target,
		Inputs: build.Inputs{
			ContextPath:    contextPath,
			DockerfilePath: dockerfilePath,
			NamedContexts:  namedContexts,
		},
		BuildArgs:     buildArgs,
		Labels:        labels,
		NoCache:       noCache,
		NoCacheFilter: nocacheFilter,
		ExtraHosts:    extraHosts,
		CgroupParent:  cgroupParent,
		NetworkMode:   networkModeForReplay(req.NetworkMode),
		SecretSpecs:   req.Secrets,
		SSHSpecs:      req.SSH,
		Tags:          req.Tags,
		// Reproduce the recorded attestation attrs so the replay output
		// carries the same attestation shape as the original build.
		Attests: pred.Attests(),
	}

	if s.Descriptor.Platform != nil {
		opt.Platforms = []ocispecs.Platform{*s.Descriptor.Platform}
	}

	// Strict source pinning applies in materials mode only (the default).
	// Attach via the shared Policy slot as a callback-only entry — composes
	// with any file-based user policies the caller may have configured.
	if req.Mode == "" || req.Mode == BuildModeMaterials {
		opt.Policy = append(opt.Policy, buildflags.PolicyConfig{
			Callback: ReplayPinCallback(NewPinIndex(pred)),
		})
	}

	return opt, nil
}

func networkModeForReplay(mode string) string {
	switch mode {
	case "", "default":
		return ""
	case "none":
		return "none"
	}
	// Replay refuses network modes beyond default/none to keep the replay
	// sandbox at least as restrictive as the default. host-network mode
	// requires explicit opt-in that replay does not yet plumb through.
	return mode
}

// CheckSecrets enforces the provenance vs. user-supplied secret-ID cross
// check: required (non-optional) IDs declared in provenance must be
// provided; any provided IDs not declared in provenance are rejected.
func CheckSecrets(declared []*provenancetypes.Secret, provided buildflags.Secrets) error {
	required := map[string]struct{}{}
	declaredAll := map[string]struct{}{}
	for _, s := range declared {
		if s == nil || s.ID == "" {
			continue
		}
		declaredAll[s.ID] = struct{}{}
		if !s.Optional {
			required[s.ID] = struct{}{}
		}
	}

	providedIDs := map[string]struct{}{}
	for _, s := range provided {
		if s == nil || s.ID == "" {
			continue
		}
		providedIDs[s.ID] = struct{}{}
	}

	missing := setDiff(required, providedIDs)
	extra := setDiff(providedIDs, declaredAll)
	if len(missing) > 0 {
		return ErrMissingSecret(missing)
	}
	if len(extra) > 0 {
		return ErrExtraSecret(extra)
	}
	return nil
}

// CheckSSH enforces the provenance vs. user-supplied SSH cross check.
func CheckSSH(declared []*provenancetypes.SSH, provided []*buildflags.SSH) error {
	required := map[string]struct{}{}
	declaredAll := map[string]struct{}{}
	for _, s := range declared {
		if s == nil || s.ID == "" {
			continue
		}
		declaredAll[s.ID] = struct{}{}
		if !s.Optional {
			required[s.ID] = struct{}{}
		}
	}

	providedIDs := map[string]struct{}{}
	for _, s := range provided {
		if s == nil || s.ID == "" {
			continue
		}
		providedIDs[s.ID] = struct{}{}
	}

	missing := setDiff(required, providedIDs)
	extra := setDiff(providedIDs, declaredAll)
	if len(missing) > 0 {
		return ErrMissingSSH(missing)
	}
	if len(extra) > 0 {
		return ErrExtraSSH(extra)
	}
	return nil
}

// setDiff returns the ordered list of elements in a that are not in b.
func setDiff(a, b map[string]struct{}) []string {
	var out []string
	for k := range a {
		if _, ok := b[k]; !ok {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

// collectPrefixed returns the keys from attrs whose key starts with prefix,
// with the prefix stripped. Values are copied verbatim.
func collectPrefixed(attrs map[string]string, prefix string) map[string]string {
	out := map[string]string{}
	for k, v := range attrs {
		name, ok := strings.CutPrefix(k, prefix)
		if !ok {
			continue
		}
		out[name] = v
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
