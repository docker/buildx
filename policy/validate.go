package policy

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"maps"
	"net/url"
	"path"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/containerd/platforms"
	"github.com/distribution/reference"
	"github.com/docker/buildx/util/sourcemeta"
	gwpb "github.com/moby/buildkit/frontend/gateway/pb"
	"github.com/moby/buildkit/solver/pb"
	moby_buildkit_v1_sourcepolicy "github.com/moby/buildkit/sourcepolicy/pb"
	"github.com/moby/buildkit/sourcepolicy/policysession"
	"github.com/moby/buildkit/util/gitutil"
	"github.com/moby/buildkit/util/gitutil/gitobject"
	"github.com/open-policy-agent/opa/v1/ast"
	"github.com/open-policy-agent/opa/v1/rego"
	"github.com/open-policy-agent/opa/v1/topdown/print"
	"github.com/opencontainers/go-digest"
	ocispecs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

type Policy struct {
	opt   Opt
	funcs []fun

	denyMu          sync.Mutex
	denyIdentifiers map[string]struct{}
}

const maxResolveIterations = 10

type state struct {
	Input    Input
	Unknowns map[string]struct{}

	ImagePins                  map[digest.Digest]struct{}
	checksumNeededForSignature *gwpb.ChecksumRequest
}

func (s *state) addUnknown(key string) {
	if s.Unknowns == nil {
		s.Unknowns = make(map[string]struct{})
	}
	s.Unknowns[key] = struct{}{}
}

type fun struct {
	decl *rego.Function
	impl func(*state) func(*rego.Rego)
}

type Opt struct {
	Files            []File
	Env              Env
	Log              func(logrus.Level, string)
	FS               func() (fs.StatFS, func() error, error)
	VerifierProvider PolicyVerifierProvider
	DefaultPlatform  *ocispecs.Platform
	SourceResolver   *sourcemeta.Resolver
}

var _ policysession.PolicyCallback = (&Policy{}).CheckPolicy

type File struct {
	Filename string
	Data     []byte
}

func NewPolicy(opt Opt) *Policy {
	p := &Policy{
		opt: opt,
	}
	p.initBuiltinFuncs()
	return p
}

func (p *Policy) log(level logrus.Level, format string, v ...any) {
	if p == nil || p.opt.Log == nil {
		return
	}
	p.opt.Log(level, fmt.Sprintf(format, v...))
}

func (p *Policy) recordDenyIdentifier(req *policysession.CheckPolicyRequest) {
	if p == nil || req == nil || req.Source == nil || req.Source.Source == nil {
		return
	}

	p.denyMu.Lock()
	defer p.denyMu.Unlock()

	if p.denyIdentifiers == nil {
		p.denyIdentifiers = make(map[string]struct{})
	}
	id := strings.TrimSpace(req.Source.Source.Identifier)
	if id == "" {
		return
	}
	p.denyIdentifiers[id] = struct{}{}
}

func (p *Policy) IsPolicyError(err error) bool {
	if p == nil || err == nil {
		return false
	}
	errText := err.Error()
	// TODO: replace this string matching with a typed BuildKit error that is
	// always attached for policy DENY decisions.
	p.denyMu.Lock()
	defer p.denyMu.Unlock()
	for id := range p.denyIdentifiers {
		pattern := fmt.Sprintf("source %q not allowed by policy: action %s", id, moby_buildkit_v1_sourcepolicy.PolicyAction_DENY.String())
		if strings.Contains(errText, pattern) {
			return true
		}
	}

	return false
}

func (p *Policy) CheckPolicy(ctx context.Context, req *policysession.CheckPolicyRequest) (*policysession.DecisionResponse, *gwpb.ResolveSourceMetaRequest, error) {
	if req.Source == nil || req.Source.Source == nil {
		return nil, nil, errors.Errorf("no source info in request")
	}

	var platform *ocispecs.Platform
	if req.Platform != nil {
		pl, err := platformFromReq(req)
		if err != nil {
			return nil, nil, err
		}
		platform = pl
	} else {
		platform = p.opt.DefaultPlatform
	}

	inp, err := SourceToInput(ctx, p.opt.VerifierProvider, req.Source, platform, p.opt.Log)
	if err != nil {
		return nil, nil, errors.Wrap(err, "failed to build policy input")
	}

	caps := &ast.Capabilities{
		Builtins: builtins(),
		Features: slices.Clone(ast.Features),
	}
	comp := ast.NewCompiler().WithCapabilities(caps).WithKeepModules(true)
	if p.opt.Log != nil {
		comp = comp.WithEnablePrintStatements(true)
	}

	builtins := make(map[string]*ast.Builtin)
	for _, f := range p.funcs {
		builtins[f.decl.Name] = &ast.Builtin{
			Name: f.decl.Name,
			Decl: f.decl.Decl,
		}
	}
	comp = comp.WithBuiltins(builtins)

	var root fs.StatFS
	var closeFS func() error
	defer func() {
		if closeFS != nil {
			closeFS()
		}
	}()

	comp = comp.WithModuleLoader(func(resolved map[string]*ast.Module) (parsed map[string]*ast.Module, err error) {
		out := make(map[string]*ast.Module)
		for k, v := range resolved {
			for _, imp := range v.Imports {
				pv := imp.Path.Value.String()
				pkgPath, ok := strings.CutPrefix(pv, "data.")
				if !ok {
					continue
				}
				fn := strings.ReplaceAll(pkgPath, ".", "/") + ".rego"
				if _, ok := resolved[fn]; !ok {
					if root == nil {
						if p.opt.FS == nil {
							return nil, errors.Errorf("no policy FS defined for import %s", pv)
						}
						f, cf, err := p.opt.FS()
						if err != nil {
							return nil, errors.Wrapf(err, "failed to get policy FS for import %s", pv)
						}
						root = f
						closeFS = cf
					}
					if _, err := root.Stat(fn); err != nil {
						return nil, errors.Wrapf(err, "import %s not found for module %s", pv, k)
					}
					dt, err := fs.ReadFile(root, fn)
					if err != nil {
						return nil, errors.Wrapf(err, "failed to read imported policy file %s for module %s", fn, k)
					}
					mod, err := ast.ParseModule(fn, string(dt))
					if err != nil {
						return nil, errors.Wrapf(err, "failed to parse imported policy file %s for module %s", fn, k)
					}
					pkgParts := strings.Split(pkgPath, ".")
					ref := ast.Ref{mod.Package.Path[0]}
					for _, p := range pkgParts {
						ref = append(ref, ast.StringTerm(p))
					}
					mod.Package = &ast.Package{Path: ref}
					out[fn] = mod
				}
			}
		}
		return out, nil
	})

	baseOpts := []func(*rego.Rego){
		rego.SetRegoVersion(ast.RegoV1),
		rego.Query("data.docker.decision"),
		rego.SkipPartialNamespace(true),
		rego.Compiler(comp),
		rego.Module(builtinPolicyModuleFilename, builtinPolicyModule),
	}
	if p.opt.Log != nil {
		baseOpts = append(baseOpts,
			rego.EnablePrintStatements(true),
			rego.PrintHook(p),
		)
	}
	for _, file := range p.opt.Files {
		baseOpts = append(baseOpts, rego.Module(file.Filename, string(file.Data)))
	}

	p.log(logrus.InfoLevel, "checking policy for source %s", sourceName(req))

	for range maxResolveIterations {
		runInput := inp
		applyEnvWithDepth(&runInput, p.opt.Env, 0)

		runOpts := append([]func(*rego.Rego){}, baseOpts...)
		runOpts = append(runOpts, rego.Input(runInput))

		st := &state{Input: runInput}
		for _, f := range p.funcs {
			runOpts = append(runOpts, f.impl(st))
		}

		dt, err := json.MarshalIndent(runInput, "", "  ")
		if err != nil {
			return nil, nil, errors.Wrapf(err, "failed to marshal policy input")
		}
		p.log(logrus.DebugLevel, "policy input: %s", dt)

		unknowns := inp.Unknowns()
		if len(unknowns) > 0 {
			p.log(logrus.DebugLevel, "unknowns for policy evaluation: %+v", summarizeUnknownsForLog(unknowns))
			runOpts = append(runOpts, rego.Unknowns(unknowns))
		}
		r := rego.New(runOpts...)

		if len(unknowns) > 0 {
			pq, err := r.Partial(ctx)
			if err != nil {
				return nil, nil, err
			}
			unk := collectUnknowns(pq.Support, unknowns)
			unk = append(unk, runtimeUnknownInputRefs(st)...)

			retry, next, err := p.resolveUnknowns(ctx, &inp, req, platform, unk, st)
			if err != nil {
				return nil, nil, err
			}
			if next != nil {
				return nil, next, nil
			}
			if retry {
				continue
			}
		}

		st.ImagePins = nil
		rs, err := r.Eval(ctx)
		if err != nil {
			return nil, nil, err
		}

		retry, next, err := p.resolveUnknowns(ctx, &inp, req, platform, runtimeUnknownInputRefs(st), st)
		if err != nil {
			return nil, nil, err
		}
		if next != nil {
			return nil, next, nil
		}
		if retry {
			continue
		}

		if len(rs) == 0 {
			return nil, nil, errors.Errorf("policy returned zero result")
		}
		rsz := rs[0]
		if len(rsz.Expressions) == 0 {
			return nil, nil, errors.Errorf("policy returned zero expressions")
		}
		v := rsz.Expressions[0].Value
		vt, ok := v.(map[string]any)
		if !ok {
			return nil, nil, errors.Errorf("unexpected policy return type: %T %s", vt, rsz.Expressions[0].Text)
		}

		resp := &policysession.DecisionResponse{
			Action: moby_buildkit_v1_sourcepolicy.PolicyAction_DENY,
		}
		p.log(logrus.DebugLevel, "policy response: %+v", vt)

		if v, ok := vt["allow"]; ok {
			if vv, ok := v.(bool); !ok {
				return nil, nil, errors.Errorf("invalid allowed property type %T, expecting bool", v)
			} else if vv {
				resp.Action = moby_buildkit_v1_sourcepolicy.PolicyAction_ALLOW
			}
		}

		if v, ok := vt["deny_msg"]; ok {
			if vv, ok := v.([]any); ok {
				for _, m := range vv {
					if m, ok := m.(string); ok {
						resp.DenyMessages = append(resp.DenyMessages, &policysession.DenyMessage{
							Message: m,
						})
					}
				}
			}
		}

		if resp.Action == moby_buildkit_v1_sourcepolicy.PolicyAction_ALLOW {
			if len(st.ImagePins) > 1 {
				return nil, nil, errors.Errorf("multiple image pins set to %s: %v", sourceName(req), st.ImagePins)
			}
			if len(st.ImagePins) == 1 {
				newSrc, err := AddPinToImage(req.Source.Source, slices.Collect(maps.Keys(st.ImagePins))[0])
				if err != nil {
					return nil, nil, errors.Wrapf(err, "failed to add image pin to source")
				}
				p.log(logrus.InfoLevel, "policy decision for source %s: convert to %s", sourceName(req), newSrc.Identifier)

				return &policysession.DecisionResponse{
					Action: moby_buildkit_v1_sourcepolicy.PolicyAction_CONVERT,
					Update: newSrc,
				}, nil, nil
			}
		}

		p.log(logrus.InfoLevel, "policy decision for source %s: %s", sourceName(req), resp.Action)
		for _, dm := range resp.DenyMessages {
			p.log(logrus.InfoLevel, " - %s", dm.Message)
		}
		if resp.Action == moby_buildkit_v1_sourcepolicy.PolicyAction_DENY {
			p.recordDenyIdentifier(req)
		}
		return resp, nil, nil
	}

	return nil, nil, errors.Errorf("maximum attempts reached for resolving policy metadata")
}

func (p *Policy) resolveUnknowns(ctx context.Context, input *Input, req *policysession.CheckPolicyRequest, defaultPlatform *ocispecs.Platform, unk []string, st *state) (bool, *gwpb.ResolveSourceMetaRequest, error) {
	var resolver SourceMetadataResolver
	if p.opt.SourceResolver != nil {
		resolver = p.opt.SourceResolver
	}

	if st != nil && st.checksumNeededForSignature != nil {
		next := &gwpb.ResolveSourceMetaRequest{
			Source: req.Source.Source,
		}
		if req.Platform != nil {
			next.Platform = req.Platform
		}
		if err := AddUnknownsWithLogger(p.opt.Log, next, normalizeNodeUnknowns(unk)); err != nil {
			return false, nil, err
		}
		next.HTTP = &gwpb.ResolveSourceHTTPRequest{
			ChecksumRequest: st.checksumNeededForSignature.CloneVT(),
		}
		p.log(logrus.InfoLevel, "policy decision for source %s: resolve missing fields %+v", sourceName(req), summarizeUnknownsForLog(unk))
		return false, next, nil
	}

	retry, next, err := ResolveInputUnknowns(ctx, input, req.Source.Source, unk, req.Platform, defaultPlatform, resolver, p.opt.VerifierProvider, p.opt.Log)
	if err != nil {
		return false, nil, err
	}
	if next != nil {
		p.log(logrus.InfoLevel, "policy decision for source %s: resolve missing fields %+v", sourceName(req), summarizeUnknownsForLog(unk))
		return false, next, nil
	}
	return retry, nil, nil
}

func platformFromReq(req *policysession.CheckPolicyRequest) (*ocispecs.Platform, error) {
	if req.Platform != nil {
		platformStr := req.Platform.OS + "/" + req.Platform.Architecture
		if req.Platform.Variant != "" {
			platformStr += "/" + req.Platform.Variant
		}
		pl, err := platforms.Parse(platformStr)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to parse platform")
		}
		pl = platforms.Normalize(pl)
		return &pl, nil
	}
	return nil, nil
}

func sourceName(req *policysession.CheckPolicyRequest) string {
	name := req.Source.Source.Identifier
	if p, _ := platformFromReq(req); p != nil {
		name += " (" + platforms.Format(*p) + ")"
	}
	return name
}

func (p *Policy) Print(ctx print.Context, msg string) error {
	if p.opt.Log != nil {
		p.opt.Log(logrus.InfoLevel, ctx.Location.Format("%s", msg))
	}
	return nil
}

func sourceToInput(ctx context.Context, getVerifier PolicyVerifierProvider, src *gwpb.ResolveSourceMetaResponse, platform *ocispecs.Platform, logf func(logrus.Level, string)) (Input, []string, error) {
	var inp Input
	var unknowns []string

	if src == nil || src.Source == nil {
		return inp, nil, errors.Errorf("no source info in request")
	}

	scheme, refstr, ok := strings.Cut(src.Source.Identifier, "://")
	if !ok {
		return inp, nil, errors.Errorf("invalid source identifier: %s", src.Source.Identifier)
	}

	switch scheme {
	case "http", "https":
		u, err := url.Parse(src.Source.Identifier)
		if err != nil {
			return inp, nil, errors.Wrapf(err, "failed to parse http source url")
		}
		inp.HTTP = &HTTP{
			URL:    src.Source.Identifier,
			Schema: scheme,
			Host:   u.Host,
			Path:   u.Path,
			Query:  u.Query(),
		}
		if src.HTTP != nil {
			inp.HTTP.Checksum = src.HTTP.Checksum
			if src.HTTP.ChecksumResponse != nil {
				inp.HTTP.checksumResponseForSignature = &httpChecksumResponseForSignature{
					Digest: src.HTTP.ChecksumResponse.Digest,
					Suffix: slices.Clone(src.HTTP.ChecksumResponse.Suffix),
				}
			}
		}
		if inp.HTTP.Checksum == "" {
			unknowns = append(unknowns, "input.http.checksum")
		}
		if _, ok := src.Source.Attrs[pb.AttrHTTPAuthHeaderSecret]; ok {
			inp.HTTP.HasAuth = true
		}
	case "git":
		if !gitutil.IsGitTransport(refstr) {
			refstr = "https://" + refstr
		}
		u, err := gitutil.ParseURL(refstr)
		if err != nil {
			return inp, nil, err
		}
		g := &Git{
			Schema: u.Scheme,
			Remote: u.Remote,
			Host:   u.Host,
		}
		var ref string
		var isFullRef bool
		if u.Opts != nil {
			ref = u.Opts.Ref
			g.Subdir = u.Opts.Subdir
			if sd := path.Clean(g.Subdir); sd == "/" || sd == "." {
				g.Subdir = ""
			}
		}
		if v, ok := src.Source.Attrs[pb.AttrFullRemoteURL]; ok {
			if !gitutil.IsGitTransport(v) {
				v = "https://" + v
			}
			u, err := gitutil.ParseURL(v)
			if err != nil {
				return inp, nil, err
			}
			g.Schema = u.Scheme
			g.Remote = u.Remote
			g.Host = u.Host
			g.FullURL = v
		}
		if tag, ok := strings.CutPrefix(ref, "refs/tags/"); ok {
			g.TagName = tag
			isFullRef = true
		}
		if branch, ok := strings.CutPrefix(ref, "refs/heads/"); ok {
			g.Branch = branch
			isFullRef = true
		}

		if gitutil.IsCommitSHA(ref) {
			g.IsCommitRef = true
			g.Checksum = ref
			g.CommitChecksum = ref
			isFullRef = true
		}

		unk := []string{}

		if src.Git == nil {
			if !isFullRef {
				unk = append(unk, "tagName", "branch", "ref")
			} else {
				g.Ref = ref
			}
			if g.Checksum == "" {
				unk = append(unk, "checksum", "isAnnotatedTag", "commitChecksum", "isSHA256")
			}
			unk = append(unk, "tag", "commit")
		} else {
			g.Ref = src.Git.Ref
			if tag, ok := strings.CutPrefix(g.Ref, "refs/tags/"); ok {
				g.TagName = tag
			}
			if branch, ok := strings.CutPrefix(g.Ref, "refs/heads/"); ok {
				g.Branch = branch
			}
			g.Checksum = src.Git.Checksum
			g.CommitChecksum = src.Git.CommitChecksum
			if g.CommitChecksum == "" {
				g.CommitChecksum = g.Checksum
			}
			if g.Checksum != g.CommitChecksum {
				g.IsAnnotatedTag = true
			}

			if len(src.Git.CommitObject) == 0 {
				unk = append(unk, "commit", "tag")
			} else {
				obj, err := gitobject.Parse(src.Git.CommitObject)
				if err != nil {
					return inp, nil, err
				}
				if err := obj.VerifyChecksum(g.CommitChecksum); err != nil {
					return inp, nil, err
				}
				c, err := obj.ToCommit()
				if err != nil {
					return inp, nil, err
				}
				g.Commit = &Commit{
					Tree:      c.Tree,
					Message:   c.Message,
					Parents:   c.Parents,
					Author:    Actor(c.Author),
					Committer: Actor(c.Committer),
					obj:       obj,
				}
				s := parseGitSignature(obj)
				g.Commit.PGPSignature = s.PGPSignature
				g.Commit.SSHSignature = s.SSHSignature

				if dt := src.Git.TagObject; len(dt) > 0 {
					obj, err := gitobject.Parse(src.Git.TagObject)
					if err != nil {
						return inp, nil, err
					}
					if err := obj.VerifyChecksum(g.Checksum); err != nil {
						return inp, nil, err
					}
					t, err := obj.ToTag()
					if err != nil {
						return inp, nil, err
					}
					g.Tag = &Tag{
						Object:  t.Object,
						Message: t.Message,
						Type:    t.Type,
						Tag:     t.Tag,
						Tagger:  Actor(t.Tagger),
						obj:     obj,
					}
					s := parseGitSignature(obj)
					g.Tag.PGPSignature = s.PGPSignature
					g.Tag.SSHSignature = s.SSHSignature
				}
			}
		}

		if len(g.Checksum) == 64 {
			g.IsSHA256 = true
		}

		unknowns = append(unknowns, withPrefix(unk, "input.git.")...)
		inp.Git = g
	case "docker-image":
		ref, err := reference.ParseNormalizedNamed(refstr)
		if err != nil {
			return inp, nil, errors.Wrapf(err, "failed to parse image source reference")
		}
		inp.Image = &Image{
			Ref:      ref.String(),
			Host:     reference.Domain(ref),
			Repo:     reference.FamiliarName(ref),
			FullRepo: ref.Name(),
		}
		if digested, ok := ref.(reference.Canonical); ok {
			inp.Image.Checksum = digested.Digest().String()
			inp.Image.IsCanonical = true
		}
		if tagged, ok := ref.(reference.Tagged); ok {
			inp.Image.Tag = tagged.Tag()
		}
		if platform == nil {
			return inp, nil, errors.Errorf("platform required for image source")
		}
		inp.Image.Platform = platforms.Format(*platform)
		inp.Image.OS = platform.OS
		inp.Image.Architecture = platform.Architecture
		inp.Image.Variant = platform.Variant

		configFields := []string{
			"labels", "user", "volumes", "workingDir", "env",
		}

		if src.Image == nil {
			if !inp.Image.IsCanonical {
				unknowns = append(unknowns, "input.image.checksum")
			}
			unknowns = append(unknowns, withPrefix(configFields, "input.image.")...)
			unknowns = append(unknowns, "input.image.hasProvenance", "input.image.provenance", "input.image.signatures")
		} else {
			inp.Image.Checksum = src.Image.Digest
			if cfg := src.Image.Config; cfg != nil {
				var img ocispecs.Image
				if err := json.Unmarshal(cfg, &img); err != nil {
					return inp, nil, errors.Wrapf(err, "failed to unmarshal image config")
				}
				if img.Created != nil {
					inp.Image.CreatedTime = img.Created.Format(time.RFC3339)
				}
				inp.Image.Labels = img.Config.Labels
				inp.Image.Env = img.Config.Env
				inp.Image.User = img.Config.User
				inp.Image.Volumes = make([]string, 0, len(img.Config.Volumes))
				for v := range img.Config.Volumes {
					inp.Image.Volumes = append(inp.Image.Volumes, v)
				}
				inp.Image.WorkingDir = img.Config.WorkingDir
			} else {
				unknowns = append(unknowns, withPrefix(configFields, "input.image.")...)
			}

			if ac := src.Image.AttestationChain; ac != nil {
				if prv, err := parseProvenance(ac, logf); err != nil {
					if logf != nil {
						logf(logrus.DebugLevel, fmt.Sprintf("failed to parse image provenance: %v", err))
					}
				} else {
					inp.Image.Provenance = prv
				}
				inp.Image.HasProvenance = ac.AttestationManifest != "" || inp.Image.Provenance != nil
				if getVerifier != nil {
					signatures, err := parseSignatures(ctx, getVerifier, ac, platform)
					if err != nil {
						if logf != nil {
							logf(logrus.DebugLevel, fmt.Sprintf("failed to parse image signatures: %v", err))
						}
					} else {
						inp.Image.Signatures = signatures
					}
				}
			} else {
				unknowns = append(unknowns, "input.image.hasProvenance", "input.image.provenance", "input.image.signatures")
			}
		}
	case "local":
		inp.Local = &Local{
			Name: refstr,
		}
	default:
		// oci-layout not supported yet
		return inp, nil, errors.Errorf("unsupported source scheme: %s", scheme)
	}

	return inp, unknowns, nil
}

func withPrefix(arr []string, prefix string) []string {
	out := make([]string, len(arr))
	for i, s := range arr {
		out[i] = prefix + s
	}
	return out
}

func applyEnvWithDepth(inp *Input, env Env, depth int) {
	if inp == nil {
		return
	}
	inp.Env = env
	inp.Env.Depth = depth
	if inp.Image == nil || inp.Image.Provenance == nil || len(inp.Image.Provenance.Materials) == 0 {
		return
	}
	for i := range inp.Image.Provenance.Materials {
		applyEnvWithDepth(&inp.Image.Provenance.Materials[i], env, depth+1)
	}
}

func AddUnknowns(req *gwpb.ResolveSourceMetaRequest, unk []string) error {
	return AddUnknownsWithLogger(nil, req, unk)
}

func AddUnknownsWithLogger(logf func(logrus.Level, string), req *gwpb.ResolveSourceMetaRequest, unk []string) error {
	unk2 := make([]string, 0, len(unk))
	for _, u := range unk {
		switch u {
		case "image", "git", "http", "local":
			// parents are returned as unknowns for some reason, ignore
			continue
		default:
			unk2 = append(unk2, u)
		}
	}
	if len(unk2) == 0 {
		return nil
	}

	if logf != nil {
		logf(logrus.DebugLevel, fmt.Sprintf("collected unknowns: %+v", unk2))
	}
	for _, u := range unk2 {
		if u == "image.provenance" || strings.HasPrefix(u, "image.provenance.") {
			if req.Image == nil {
				req.Image = &gwpb.ResolveSourceImageRequest{
					NoConfig: true,
				}
			}
			req.Image.AttestationChain = true
			req.Image.ResolveAttestations = appendUnique(req.Image.ResolveAttestations, resolveProvenanceAttestations...)
			continue
		}

		switch u {
		case "image.checksum", "image.labels", "image.user", "image.volumes", "image.workingDir", "image.env":
			if req.Image == nil {
				req.Image = &gwpb.ResolveSourceImageRequest{}
			}
			req.Image.NoConfig = false
		case "image.hasProvenance", "image.signatures":
			if req.Image == nil {
				req.Image = &gwpb.ResolveSourceImageRequest{
					NoConfig: true,
				}
			}
			req.Image.AttestationChain = true

		case "http.checksum":
			// HTTP checksums are resolved by BuildKit for the HTTP source itself.

		case "git.ref", "git.checksum", "git.commitChecksum", "git.isAnnotatedTag", "git.isSHA256", "git.tagName", "git.branch":
			if req.Git == nil {
				req.Git = &gwpb.ResolveSourceGitRequest{}
			}
		case "git.commit", "git.tag":
			if req.Git == nil {
				req.Git = &gwpb.ResolveSourceGitRequest{}
			}
			req.Git.ReturnObject = true

		default:
			return errors.Errorf("unhandled unknown property %s", u)
		}
	}
	return nil
}

func collectUnknowns(mods []*ast.Module, allowed []string) []string {
	seen := map[string]struct{}{}
	var out []string

	for _, mod := range mods {
		ast.WalkRefs(mod, func(ref ast.Ref) bool {
			if ref.HasPrefix(ast.InputRootRef) {
				s := trimKey(ref.String())
				if s == "" {
					return true
				}
				if _, ok := seen[s]; !ok {
					seen[s] = struct{}{}
					out = append(out, s)
				}
			}
			return true
		})
	}
	if allowed == nil {
		return out
	}

	valid := map[string]struct{}{}
	for _, k := range allowed {
		k = trimKey(k)
		if k == "" {
			continue
		}
		valid[k] = struct{}{}
	}

	filtered := make([]string, 0, len(out))
	filteredSeen := map[string]struct{}{}
	for _, k := range out {
		matched, ok := matchAllowedOrParent(k, valid)
		if !ok {
			continue
		}
		if _, exists := filteredSeen[matched]; exists {
			continue
		}
		filteredSeen[matched] = struct{}{}
		filtered = append(filtered, matched)
	}

	return filtered
}

func matchAllowedOrParent(key string, allowed map[string]struct{}) (string, bool) {
	if _, ok := allowed[key]; ok {
		return key, true
	}
	// Find the nearest parent on a component boundary.
	for i := len(key) - 1; i >= 0; i-- {
		switch key[i] {
		case '.', '[':
			if i == 0 {
				continue
			}
			candidate := key[:i]
			if _, ok := allowed[candidate]; ok {
				return candidate, true
			}
		}
	}
	return "", false
}

func runtimeUnknownInputRefs(st *state) []string {
	if st == nil || len(st.Unknowns) == 0 {
		return nil
	}
	var out []string
	if _, ok := st.Unknowns[funcVerifyGitSignature]; ok {
		out = append(out, "git.commit")
	}
	if _, ok := st.Unknowns[funcArtifactAttestation]; ok {
		out = append(out, "http.checksum")
	} else if _, ok := st.Unknowns[funcGithubAttestation]; ok {
		out = append(out, "http.checksum")
	}
	return out
}

func summarizeUnknownsForLog(unk []string) []string {
	out := make([]string, 0, len(unk))
	seen := map[string]struct{}{}
	for _, u := range unk {
		u = strings.TrimPrefix(u, "input.")
		if strings.HasPrefix(u, "image.signatures") {
			u = "image.signatures"
		}
		if strings.HasPrefix(u, "image.provenance") {
			u = "image.provenance"
		}
		if u == "image" {
			continue
		}
		if _, ok := seen[u]; ok {
			continue
		}
		seen[u] = struct{}{}
		out = append(out, u)
	}
	return out
}

func appendUnique(dst []string, values ...string) []string {
	for _, v := range values {
		if !slices.Contains(dst, v) {
			dst = append(dst, v)
		}
	}
	return dst
}

func hasHTTPUnknowns(unk []string) bool {
	for _, u := range unk {
		if strings.HasPrefix(u, "http.") {
			return true
		}
	}
	return false
}

func trimKey(s string) string {
	s = strings.TrimPrefix(s, "input.")
	if strings.HasPrefix(s, "image.provenance.materials[") {
		return s
	}

	const (
		dot = '.'
		sb  = '['
	)

	components := 0
	for i, r := range s {
		if r == dot || r == sb {
			components++
			if components == 2 {
				return s[:i]
			}
		}
	}
	return s
}

func funcNoInput(f func(*rego.Rego)) func(*state) func(*rego.Rego) {
	return func(_ *state) func(*rego.Rego) {
		return f
	}
}
