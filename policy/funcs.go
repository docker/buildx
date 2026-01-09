package policy

import (
	"encoding/json"
	"io"
	"maps"
	"strings"

	"github.com/distribution/reference"
	"github.com/moby/buildkit/solver/pb"
	"github.com/moby/buildkit/util/gitutil/gitsign"
	"github.com/open-policy-agent/opa/v1/ast"
	"github.com/open-policy-agent/opa/v1/rego"
	"github.com/open-policy-agent/opa/v1/types"
	"github.com/opencontainers/go-digest"
	"github.com/pkg/errors"
)

const (
	funcLoadJSON           = "load_json"
	funcVerifyGitSignature = "verify_git_signature"
	funcPinImage           = "pin_image"
)

func (p *Policy) initBuiltinFuncs() {
	builtinLoadJSON := &rego.Function{
		Name: funcLoadJSON,
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
		Name: funcVerifyGitSignature,
		Decl: types.NewFunction(
			types.Args(
				types.A,
				types.S,
			),
			types.B,
		),
		Memoize: false, // TODO:optimize
	}
	p.funcs = append(p.funcs, fun{
		decl: verifyGitSignature,
		impl: func(s *state) func(*rego.Rego) {
			return rego.Function2(verifyGitSignature, func(bctx rego.BuiltinContext, a1 *ast.Term, a2 *ast.Term) (*ast.Term, error) {
				return p.builtinVerifyGitSignatureImpl(bctx, a1, a2, s)
			})
		},
	})

	pinImageDigest := &rego.Function{
		Name: funcPinImage,
		Decl: types.NewFunction(
			types.Args(
				types.A,
				types.S,
			),
			types.B,
		),
		Memoize: false, // TODO: optimize
	}

	p.funcs = append(p.funcs, fun{
		decl: pinImageDigest,
		impl: func(s *state) func(*rego.Rego) {
			return rego.Function2(pinImageDigest, func(bctx rego.BuiltinContext, a1 *ast.Term, a2 *ast.Term) (*ast.Term, error) {
				return p.builtinPinImageImpl(bctx, a1, a2, s)
			})
		},
	})
}

func (p *Policy) builtinPinImageImpl(_ rego.BuiltinContext, a1, a2 *ast.Term, s *state) (*ast.Term, error) {
	inp := s.Input
	if inp.Image == nil {
		return ast.BooleanTerm(false), nil
	}

	obja, ok := a1.Value.(ast.Object)
	if !ok {
		return nil, errors.Errorf("%s: expected object, got %T", funcPinImage, a1.Value)
	}

	imageValue, err := ast.InterfaceToValue(inp.Image)
	if err != nil {
		return nil, errors.Wrapf(err, "%s: failed converting object to interface", funcPinImage)
	}

	if obja.Compare(imageValue) != 0 {
		return nil, errors.Errorf("%s: first argument is not the same as input image", funcPinImage)
	}

	dgstStr, ok := a2.Value.(ast.String)
	if !ok {
		return nil, errors.Errorf("%s: expected string path, got %T", funcPinImage, a2.Value)
	}

	dgst, err := digest.Parse(string(dgstStr))
	if err != nil {
		return nil, errors.Wrapf(err, "%s: invalid digest", funcPinImage)
	}

	if inp.Image.Checksum == string(dgst) {
		return ast.BooleanTerm(true), nil
	}

	if s.ImagePins == nil {
		s.ImagePins = make(map[digest.Digest]struct{})
	}
	s.ImagePins[dgst] = struct{}{}

	return ast.BooleanTerm(true), nil
}

func (p *Policy) builtinVerifyGitSignatureImpl(_ rego.BuiltinContext, a1, a2 *ast.Term, s *state) (*ast.Term, error) {
	inp := s.Input
	if inp.Git == nil {
		return ast.BooleanTerm(false), nil
	}

	if inp.Git.Commit == nil {
		s.addUnknown(funcVerifyGitSignature)
		return ast.BooleanTerm(false), nil
	}

	obja, ok := a1.Value.(ast.Object)
	if !ok {
		return nil, errors.Errorf("%s: expected object, got %T", funcVerifyGitSignature, a1.Value)
	}

	commitValue, err := ast.InterfaceToValue(inp.Git.Commit)
	if err != nil {
		return nil, errors.Wrapf(err, "%s: failed converting object to interface", funcVerifyGitSignature)
	}
	isCommit := obja.Compare(commitValue) == 0

	var isTag bool
	if !isCommit && inp.Git.Tag != nil {
		tagValue, err := ast.InterfaceToValue(inp.Git.Tag)
		if err != nil {
			return nil, errors.Wrapf(err, "%s: failed converting object to interface", funcVerifyGitSignature)
		}
		isTag = obja.Compare(tagValue) == 0
	}

	if !isCommit && !isTag {
		return nil, errors.Errorf("%s: object is neither commit nor tag", funcVerifyGitSignature)
	}

	path, ok := a2.Value.(ast.String)
	if !ok {
		return nil, errors.Errorf("%s: expected string path, got %T", funcVerifyGitSignature, a2.Value)
	}

	pubkey, err := p.readFile(string(path), 128*1024)
	if err != nil {
		return nil, err
	}

	obj := inp.Git.Commit.obj
	if isTag {
		obj = inp.Git.Tag.obj
	}

	if err := gitsign.VerifySignature(obj, pubkey, nil); err != nil {
		return nil, errors.Wrapf(err, "%s: verification failes", funcVerifyGitSignature)
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
		return nil, errors.Errorf("%s: expected string path, got %T", funcLoadJSON, a.Value)
	}

	data, err := p.readFile(string(path), 4*1024*1024)
	if err != nil {
		return nil, err
	}

	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		return nil, errors.Wrapf(err, "%s: invalid JSON in %q", funcLoadJSON, path)
	}

	astVal, err := ast.InterfaceToValue(v)
	if err != nil {
		return nil, errors.Wrapf(err, "%s: failed converting JSON from %q", funcLoadJSON, path)
	}

	return ast.NewTerm(astVal), nil
}

func addPinToImage(src *pb.SourceOp, dgst digest.Digest) (*pb.SourceOp, error) {
	id, ok := strings.CutPrefix(src.Identifier, "docker-image://")
	if !ok {
		return nil, errors.Errorf("cannot pin non-image source: %q", src.Identifier)
	}

	ref, err := reference.ParseNormalizedNamed(id)
	if err != nil {
		return nil, errors.Wrapf(err, "failed parsing image reference %q", id)
	}

	newRef, err := reference.WithDigest(ref, dgst)
	if err != nil {
		return nil, errors.Wrapf(err, "failed adding digest to image reference %q", id)
	}
	attrs := maps.Clone(src.Attrs)
	if attrs == nil {
		attrs = make(map[string]string)
	}
	attrs["image.checksum"] = dgst.String()

	return &pb.SourceOp{
		Identifier: "docker-image://" + newRef.String(),
		Attrs:      attrs,
	}, nil
}
