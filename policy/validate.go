package policy

import (
	"context"
	"encoding/json"
	"io"
	"io/fs"
	"log"
	"net/url"
	"os"
	"path"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/containerd/platforms"
	"github.com/distribution/reference"
	gwpb "github.com/moby/buildkit/frontend/gateway/pb"
	"github.com/moby/buildkit/solver/pb"
	moby_buildkit_v1_sourcepolicy "github.com/moby/buildkit/sourcepolicy/pb"
	"github.com/moby/buildkit/sourcepolicy/policysession"
	"github.com/moby/buildkit/util/gitutil"
	"github.com/moby/buildkit/util/gitutil/gitobject"
	"github.com/moby/buildkit/util/gitutil/gitsign"
	"github.com/open-policy-agent/opa/v1/ast"
	"github.com/open-policy-agent/opa/v1/rego"
	"github.com/open-policy-agent/opa/v1/topdown/print"
	"github.com/open-policy-agent/opa/v1/types"
	ocispecs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/pkg/errors"
)

// this is tempory debug, to be replaced with progressbar logging later
var isDebug = sync.OnceValue(func() bool {
	if v, ok := os.LookupEnv("BUILDX_POLICY_DEBUG"); ok {
		b, _ := strconv.ParseBool(v)
		return b
	}
	return false
})

func debugf(format string, v ...any) {
	if isDebug() {
		log.Printf(format, v...)
	}
}

type Policy struct {
	opt   Opt
	funcs []fun
}

type state struct {
	Input    Input
	Unknowns map[string]struct{}
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
	Files []File
	Env   Env
	Log   func(string)
	FS    func() (fs.StatFS, func() error, error)
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

func (p *Policy) initBuiltinFuncs() {
	builtinLoadJSON := &rego.Function{
		Name: "load_json",
		Decl: types.NewFunction(
			types.Args(
				types.S,
			),
			types.A,
		),
		Memoize: true,
	}
	p.funcs = append(p.funcs, fun{
		decl: builtinLoadJSON,
		impl: funcNoInput(rego.Function1(builtinLoadJSON, p.builtinLoadJSONImpl)),
	})

	verifyGitSignature := &rego.Function{
		Name: "verify_git_signature",
		Decl: types.NewFunction(
			types.Args(
				types.S,
			),
			types.B,
		),
		Memoize: false, // TODO:optimize
	}
	p.funcs = append(p.funcs, fun{
		decl: verifyGitSignature,
		impl: func(s *state) func(*rego.Rego) {
			return rego.Function1(verifyGitSignature, func(bctx rego.BuiltinContext, a *ast.Term) (*ast.Term, error) {
				return p.builtinVerifyGitSignatureImpl(bctx, a, s)
			})
		},
	})
}

func (p *Policy) builtinVerifyGitSignatureImpl(_ rego.BuiltinContext, a *ast.Term, s *state) (*ast.Term, error) {
	inp := s.Input
	if inp.Git == nil {
		return ast.BooleanTerm(false), nil
	}

	if inp.Git.Commit == nil {
		s.addUnknown("verify_git_signature")
		return ast.BooleanTerm(false), nil
	}

	path, ok := a.Value.(ast.String)
	if !ok {
		return nil, errors.Errorf("load_json: expected string path, got %T", a.Value)
	}

	pubkey, err := p.readFile(string(path), 128*1024)
	if err != nil {
		return nil, err
	}

	obj := inp.Git.Commit.obj
	if inp.Git.Tag != nil {
		obj = inp.Git.Tag.obj
	}

	if err := gitsign.VerifySignature(obj, pubkey, &gitsign.VerifyPolicy{
		RejectExpiredKeys: false,
	}); err != nil {
		return nil, err
	}

	return ast.BooleanTerm(true), nil
}

func (p *Policy) readFile(path string, limit int64) ([]byte, error) {
	if p.opt.FS == nil {
		return nil, errors.Errorf("no policy FS defined for reading context files")
	}
	fs, cf, err := p.opt.FS()
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get policy FS for reading context files")
	}
	defer cf()

	f, err := fs.Open(path)
	if err != nil {
		return nil, errors.Wrapf(err, "failed opening file %q", path)
	}
	defer f.Close()

	rdr := io.LimitReader(f, limit)
	data, err := io.ReadAll(rdr)
	if err != nil {
		return nil, errors.Wrapf(err, "failed reading %q", path)
	}
	return data, nil
}

func (p *Policy) builtinLoadJSONImpl(bctx rego.BuiltinContext, a *ast.Term) (*ast.Term, error) {
	path, ok := a.Value.(ast.String)
	if !ok {
		return nil, errors.Errorf("load_json: expected string path, got %T", a.Value)
	}

	data, err := p.readFile(string(path), 4*1024*1024)
	if err != nil {
		return nil, err
	}

	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		return nil, errors.Wrapf(err, "load_json: invalid JSON in %q", path)
	}

	astVal, err := ast.InterfaceToValue(v)
	if err != nil {
		return nil, errors.Wrapf(err, "load_json: failed converting JSON from %q", path)
	}

	return ast.NewTerm(astVal), nil
}

func (p *Policy) CheckPolicy(ctx context.Context, req *policysession.CheckPolicyRequest) (*policysession.DecisionResponse, *gwpb.ResolveSourceMetaRequest, error) {
	var inp Input
	var unknowns []string
	inp.Env = p.opt.Env

	if req.Source == nil || req.Source.Source == nil {
		return nil, nil, errors.Errorf("no source info in request")
	}
	src := req.Source

	scheme, refstr, ok := strings.Cut(src.Source.Identifier, "://")
	if !ok {
		return nil, nil, errors.Errorf("invalid source identifier: %s", src.Source.Identifier)
	}

	switch scheme {
	case "http", "https":
		u, err := url.Parse(src.Source.Identifier)
		if err != nil {
			return nil, nil, errors.Wrapf(err, "failed to parse http source url")
		}
		inp.HTTP = &HTTP{
			URL:    src.Source.Identifier,
			Schema: scheme,
			Host:   u.Host,
			Path:   u.Path,
			Query:  u.Query(),
		}
		if _, ok := src.Source.Attrs[pb.AttrHTTPAuthHeaderSecret]; ok {
			inp.HTTP.HasAuth = true
		}
		if req.Source.Image == nil {
			unknowns = append(unknowns, "input.http.checksum")
		} else {
			inp.HTTP.Checksum = req.Source.Image.Digest
		}
	case "git":
		if !gitutil.IsGitTransport(refstr) {
			refstr = "https://" + refstr
		}
		u, err := gitutil.ParseURL(refstr)
		if err != nil {
			return nil, nil, err
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
		if v, ok := src.Source.Attrs[pb.AttrFullRemoteURL]; !ok {
			if !gitutil.IsGitTransport(v) {
				v = "https://" + v
			}
			u, err := gitutil.ParseURL(v)
			if err != nil {
				return nil, nil, err
			}
			g.Schema = u.Scheme
			g.Remote = u.Remote
			g.Host = u.Host
			g.FullURL = v
		}
		if tag, ok := strings.CutPrefix(g.Ref, "refs/tags/"); ok {
			g.TagName = tag
			isFullRef = true
		}
		if branch, ok := strings.CutPrefix(g.Ref, "refs/heads/"); ok {
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
					return nil, nil, err
				}
				if err := obj.VerifyChecksum(g.CommitChecksum); err != nil {
					return nil, nil, err
				}
				c, err := obj.ToCommit()
				if err != nil {
					return nil, nil, err
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
						return nil, nil, err
					}
					if err := obj.VerifyChecksum(g.Checksum); err != nil {
						return nil, nil, err
					}
					t, err := obj.ToTag()
					if err != nil {
						return nil, nil, err
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
			return nil, nil, errors.Wrapf(err, "failed to parse image source reference")
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
		if req.Platform == nil {
			return nil, nil, errors.Errorf("platform required for image source")
		}
		platformStr := req.Platform.OS + "/" + req.Platform.Architecture
		if req.Platform.Variant != "" {
			platformStr += "/" + req.Platform.Variant
		}
		p, err := platforms.Parse(platformStr)
		if err != nil {
			return nil, nil, errors.Wrapf(err, "failed to parse platform")
		}
		p = platforms.Normalize(p)
		inp.Image.Platform = platforms.Format(p)
		inp.Image.OS = p.OS
		inp.Image.Architecture = p.Architecture
		inp.Image.Variant = p.Variant

		configFields := []string{
			"checksum", "labels", "user", "volumes", "workingDir", "env",
		}

		if req.Source.Image == nil {
			if !inp.Image.IsCanonical {
				unknowns = append(unknowns, "input.image.checksum")
			}
			unknowns = append(unknowns, withPrefix(configFields, "input.image.")...)
			unknowns = append(unknowns, "input.image.hasProvenance")
		} else {
			inp.Image.Checksum = req.Source.Image.Digest
			if cfg := req.Source.Image.Config; cfg != nil {
				var img ocispecs.Image
				if err := json.Unmarshal(cfg, &img); err != nil {
					return nil, nil, errors.Wrapf(err, "failed to unmarshal image config")
				}
				inp.Image.CreatedTime = img.Created.Format(time.RFC3339)
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

			if ac := req.Source.Image.AttestationChain; ac != nil {
				inp.Image.HasProvenance = ac.AttestationManifest != ""
			} else {
				unknowns = append(unknowns, "input.image.hasProvenance")
			}
		}
		unknowns = append(unknowns, "input.image.signatures")
	case "local":
		inp.Local = &Local{
			Name: refstr,
		}
	default:
		// oci-layout not supported yet
		return nil, nil, errors.Errorf("unsupported source scheme: %s", scheme)
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
					// rewrite package to be less strict
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

	opts := []func(*rego.Rego){
		rego.SetRegoVersion(ast.RegoV1),
		rego.Query("data.docker.decision"),
		rego.Input(inp),
		rego.SkipPartialNamespace(true),
		rego.Compiler(comp),
	}
	if p.opt.Log != nil {
		opts = append(opts,
			rego.EnablePrintStatements(true),
			rego.PrintHook(p),
		)
	}
	st := &state{
		Input: inp,
	}
	for _, f := range p.funcs {
		opts = append(opts, f.impl(st))
	}

	for _, file := range p.opt.Files {
		opts = append(opts, rego.Module(file.Filename, string(file.Data)))
	}
	dt, err := json.MarshalIndent(inp, "", "  ")
	if err != nil {
		return nil, nil, errors.Wrapf(err, "failed to marshal policy input")
	}
	debugf("policy input: %s", dt)

	if len(unknowns) > 0 {
		debugf("unknowns for policy evaluation: %+v", unknowns)
		opts = append(opts, rego.Unknowns(unknowns))
	}
	r := rego.New(opts...)

	if len(unknowns) > 0 {
		pq, err := r.Partial(ctx)
		if err != nil {
			return nil, nil, err
		}
		unk := collectUnknowns(pq.Support)
		if _, ok := st.Unknowns["verify_git_signature"]; ok {
			unk = append(unk, "input.git.commit")
		}
		if len(unk) > 0 {
			next := &gwpb.ResolveSourceMetaRequest{
				Source:   req.Source.Source,
				Platform: req.Platform,
			}
			unk2 := make([]string, 0, len(unk))
			for _, u := range unk {
				k := strings.TrimPrefix(u, "input.")
				k = trimKey(k)
				switch k {
				case "image", "git", "http", "local":
					// parents are returned as unknowns for some reason, ignore
					continue
				default:
					unk2 = append(unk2, k)
				}
			}
			if len(unk2) > 0 {
				debugf("collected unknowns: %+v", unk2)
				for _, u := range unk2 {
					switch u {
					case "image.labels", "image.user", "image.volumes", "image.workingDir", "image.env":
						if next.Image == nil {
							next.Image = &gwpb.ResolveSourceImageRequest{}
						}
						next.Image.NoConfig = false
					case "image.hasProvenance":
						if next.Image == nil {
							next.Image = &gwpb.ResolveSourceImageRequest{
								NoConfig: true,
							}
						}
						next.Image.AttestationChain = true
					case "image.checksum":

					case "git.ref", "git.checksum", "git.commitChecksum", "git.isAnnotatedTag", "git.isSHA256", "git.tagName", "git.branch":

					case "git.commit", "git.tag":
						if next.Git == nil {
							next.Git = &gwpb.ResolveSourceGitRequest{}
						}
						next.Git.ReturnObject = true

					default:
						return nil, nil, errors.Errorf("unhandled unknown property %s", u)
					}
				}
				debugf("next resolve meta request: %+v", next)
				return nil, next, nil
			}
		}
	}

	rs, err := r.Eval(ctx)
	if err != nil {
		return nil, nil, err
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
	debugf("policy response: %+v", vt)

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
	debugf("policy decision: %s %v", resp.Action, resp.DenyMessages)

	return resp, nil, nil
}

func (p *Policy) Print(ctx print.Context, msg string) error {
	if p.opt.Log != nil {
		p.opt.Log(ctx.Location.Format("%s", msg))
	}
	return nil
}

func withPrefix(arr []string, prefix string) []string {
	out := make([]string, len(arr))
	for i, s := range arr {
		out[i] = prefix + s
	}
	return out
}

func collectUnknowns(mods []*ast.Module) []string {
	seen := map[string]struct{}{}
	var out []string

	for _, mod := range mods {
		ast.WalkRefs(mod, func(ref ast.Ref) bool {
			if ref.HasPrefix(ast.InputRootRef) {
				s := ref.String() // e.g. "input.request.path"
				if _, ok := seen[s]; !ok {
					seen[s] = struct{}{}
					out = append(out, s)
				}
			}
			return true
		})
	}
	return out
}

func trimKey(s string) string {
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
