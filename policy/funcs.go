package policy

import (
	"encoding/json"
	"io"
	"log"

	"github.com/moby/buildkit/util/gitutil/gitsign"
	"github.com/open-policy-agent/opa/v1/ast"
	"github.com/open-policy-agent/opa/v1/rego"
	"github.com/open-policy-agent/opa/v1/types"
	"github.com/pkg/errors"
)

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
		log.Printf("git signature verification failed: %+v", err)
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
