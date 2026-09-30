package replay

import (
	"context"
	"fmt"
	"maps"
	"net/url"
	"slices"
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
	"github.com/moby/buildkit/client"
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
	// replayed build (default | none). Empty uses the recorded mode.
	// Material resolution is NOT affected.
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

// checkBuildRequest runs the pre-solve checks shared by Build and
// MakeBuildPlan so that a dry-run fails exactly like the real replay would.
func checkBuildRequest(req *BuildRequest) error {
	if len(req.Targets) == 0 {
		return errors.New("no targets to replay")
	}
	if len(req.Targets) > 1 {
		return ErrNotImplemented("replaying more than one platform at a time; select a single platform with --platform")
	}
	for _, t := range req.Targets {
		if t.Subject == nil || t.Predicate == nil {
			return errors.New("target has nil subject or predicate")
		}
		if err := checkReplayable(t.Predicate, req.Mode, req.Secrets, req.SSH); err != nil {
			return err
		}
	}
	return nil
}

// createExports parses the export specs. Local exports with mode=delete are
// rejected: `buildx build` requires --allow=buildx.local.delete for most
// destinations, and replay has no --allow flag.
func createExports(specs []*buildflags.ExportEntry) ([]client.ExportEntry, error) {
	exports, _, err := build.CreateExports(specs)
	if err != nil {
		return nil, errors.Wrap(err, "parse --output")
	}
	for _, e := range exports {
		if e.Type != client.ExporterLocal {
			continue
		}
		mode, err := client.ParseLocalExporterMode(e.Attrs["mode"])
		if err != nil {
			return nil, err
		}
		if mode == client.LocalExporterModeDelete {
			return nil, errors.New("replay does not support local output mode=delete")
		}
	}
	return exports, nil
}

// checkReplayable rejects provenance that cannot be replayed faithfully and
// cross-checks the user-supplied secrets and SSH against the recorded ones.
func checkReplayable(pred *Predicate, mode BuildMode, secrets buildflags.Secrets, ssh []*buildflags.SSH) error {
	if names := localInputs(pred); len(names) > 0 {
		return ErrUnreplayableLocalContext(names)
	}
	if pred.IsMinMode() {
		return ErrMinModeProvenance()
	}
	if mode == "" || mode == BuildModeMaterials {
		if err := checkContextPinned(pred); err != nil {
			return err
		}
	}
	if err := CheckSecrets(pred.Secrets(), secrets); err != nil {
		return err
	}
	return CheckSSH(pred.SSH(), ssh)
}

// localInputs returns the names of recorded inputs that only existed on the
// original client: local directories, a context read from stdin, and named
// contexts that referenced an OCI layout store or another bake target.
func localInputs(pred *Predicate) []string {
	seen := map[string]struct{}{}
	for _, l := range pred.Locals() {
		seen[l.Name] = struct{}{}
	}
	attrs := pred.FrontendAttrs()
	// A context read from stdin is uploaded through the client session and
	// recorded as http://buildkit-session/<id>.
	for _, uri := range []string{pred.ConfigSource().URI, attrs["context"]} {
		if u, err := url.Parse(uri); err == nil && u.Host == "buildkit-session" {
			seen["context (stdin)"] = struct{}{}
		}
	}
	for k, v := range attrs {
		name, ok := strings.CutPrefix(k, "context:")
		if !ok {
			continue
		}
		for _, prefix := range []string{"oci-layout://", "local:", "input:", "target:"} {
			if strings.HasPrefix(v, prefix) {
				seen[name] = struct{}{}
				break
			}
		}
	}
	return slices.Sorted(maps.Keys(seen))
}

// checkContextPinned rejects a Git subdirectory context whose commit is not
// recorded. BuildKit before v0.25 recorded no digest for the build context
// and recorded the Git material without the subdirectory, so the context
// request cannot be matched to a pinned material and would only fail during
// the solve.
func checkContextPinned(pred *Predicate) error {
	cfg := pred.ConfigSource()
	if cfg.URI == "" || len(cfg.Digest) > 0 {
		return nil
	}
	if _, fragment, ok := strings.Cut(cfg.URI, "#"); !ok || !strings.Contains(fragment, ":") {
		return nil
	}
	if _, ok := NewPinIndex(pred).Lookup(cfg.URI); ok {
		return nil
	}
	return ErrUnpinnedContext(cfg.URI)
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
func Build(ctx context.Context, dockerCli command.Cli, builderName string, req *BuildRequest) (retErr error) {
	if req == nil {
		return errors.New("nil build request")
	}
	if err := checkBuildRequest(req); err != nil {
		return err
	}

	// Parse exports once; shared across all targets.
	exports, err := createExports(req.Exports)
	if err != nil {
		return err
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
	// Keep the printer alive while cancellation propagates through BuildKit.
	// If it shares the solve context, Ctrl-C stops its reader before producers
	// finish and a late progress write can block solve cleanup indefinitely.
	printerCtx, cancelPrinter := context.WithCancelCause(context.TODO())
	defer func() { cancelPrinter(errors.WithStack(context.Canceled)) }()
	printer, err := progress.NewPrinter(printerCtx, dockerCli.Err(), progressMode,
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

	if _, err := build.Build(ctx, nodes, buildOpts, dockerutil.NewClient(dockerCli), confutil.NewConfig(dockerCli), printer, nil); err != nil {
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
	if req == nil {
		return build.Options{}, errors.New("nil build request")
	}
	if req.Materials.HasExplicitSources() {
		return build.Options{}, ErrNotImplemented("replay build with explicit --materials sources")
	}

	attrs := pred.FrontendAttrs()
	networkMode, err := networkModeForReplay(req.NetworkMode, attrs["force-network-mode"])
	if err != nil {
		return build.Options{}, err
	}

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

	frontend := pred.Frontend()
	frontendAttrs := map[string]string{}
	if frontend == "gateway.v0" {
		if source := attrs["source"]; source != "" {
			frontendAttrs["source"] = source
		}
		if cmdline := strings.TrimSpace(attrs["cmdline"]); cmdline != "" {
			frontendAttrs["cmdline"] = cmdline
			if frontendAttrs["source"] == "" {
				frontendAttrs["source"] = strings.Fields(cmdline)[0]
			}
		}
		if frontendAttrs["source"] == "" {
			return build.Options{}, errors.New("gateway.v0 predicate has no recorded frontend source")
		}
	}

	opt := build.Options{
		Ref:           identity.NewID(),
		Frontend:      frontend,
		FrontendAttrs: frontendAttrs,
		Target:        target,
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
		NetworkMode:   networkMode,
		SecretSpecs:   req.Secrets,
		SSHSpecs:      req.SSH,
		Tags:          req.Tags,
		// A replay provenance attestation would describe the replay operation,
		// not the original build. Disable default provenance/SBOM attachment.
		Attests: map[string]*string{"provenance": nil, "sbom": nil},
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

// networkModeForReplay returns the network mode for RUN instructions. An
// explicit mode takes precedence. Otherwise the recorded mode is kept. A
// recorded host network needs an entitlement that replay cannot grant, so it
// requires an explicit mode.
func networkModeForReplay(mode, recorded string) (string, error) {
	switch mode {
	case "":
		switch recorded {
		case "", "default":
			return "", nil
		case "none":
			return "none", nil
		case "host":
			return "", errors.New("the original build used --network=host, which replay does not support; pass --network=default or --network=none to replay with a different network mode")
		}
		return "", errors.Errorf("unsupported recorded network mode %q; pass --network=default or --network=none", recorded)
	case "default":
		return "", nil
	case "none":
		return "none", nil
	default:
		return "", errors.Errorf("unsupported replay network mode %q (want default or none)", mode)
	}
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
