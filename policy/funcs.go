package policy

import (
	"bytes"
	"context"
	"crypto"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/url"
	"slices"
	"strings"

	"github.com/distribution/reference"
	"github.com/golang/snappy"
	slsa1 "github.com/in-toto/in-toto-golang/in_toto/slsa_provenance/v1"
	"github.com/moby/buildkit/client/llb"
	gwclient "github.com/moby/buildkit/frontend/gateway/client"
	gwpb "github.com/moby/buildkit/frontend/gateway/pb"
	"github.com/moby/buildkit/solver/pb"
	"github.com/moby/buildkit/util/gitutil/gitsign"
	"github.com/moby/buildkit/util/pgpsign"
	"github.com/open-policy-agent/opa/v1/ast"
	"github.com/open-policy-agent/opa/v1/rego"
	"github.com/open-policy-agent/opa/v1/types"
	"github.com/opencontainers/go-digest"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

const (
	funcLoadJSON               = "load_json"
	funcVerifyGitSignature     = "verify_git_signature"
	funcVerifyHTTPPGPSignature = "verify_http_pgp_signature"
	funcPinImage               = "pin_image"
	funcArtifactAttestation    = "artifact_attestation"
	funcGithubAttestation      = "github_attestation"
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

	verifyHTTPPGPSignature := &rego.Function{
		Name: funcVerifyHTTPPGPSignature,
		Decl: types.NewFunction(
			types.Args(
				types.A,
				types.S,
				types.S,
			),
			types.B,
		),
		Memoize: false, // TODO:optimize
	}
	p.funcs = append(p.funcs, fun{
		decl: verifyHTTPPGPSignature,
		impl: func(s *state) func(*rego.Rego) {
			return rego.Function3(verifyHTTPPGPSignature, func(bctx rego.BuiltinContext, a1 *ast.Term, a2 *ast.Term, a3 *ast.Term) (*ast.Term, error) {
				return p.builtinVerifyHTTPPGPSignatureImpl(bctx, a1, a2, a3, s)
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

	artifactAttestation := &rego.Function{
		Name: funcArtifactAttestation,
		Decl: types.NewFunction(
			types.Args(
				types.A,
				types.S,
			),
			types.A,
		),
		Memoize: false,
	}
	p.funcs = append(p.funcs, fun{
		decl: artifactAttestation,
		impl: func(s *state) func(*rego.Rego) {
			return rego.Function2(artifactAttestation, func(bctx rego.BuiltinContext, a1 *ast.Term, a2 *ast.Term) (*ast.Term, error) {
				return p.builtinArtifactAttestationImpl(bctx, a1, a2, s)
			})
		},
	})

	githubAttestation := &rego.Function{
		Name: funcGithubAttestation,
		Decl: types.NewFunction(
			types.Args(
				types.A,
				types.S,
			),
			types.A,
		),
		Memoize: false,
	}
	p.funcs = append(p.funcs, fun{
		decl: githubAttestation,
		impl: func(s *state) func(*rego.Rego) {
			return rego.Function2(githubAttestation, func(bctx rego.BuiltinContext, a1 *ast.Term, a2 *ast.Term) (*ast.Term, error) {
				return p.builtinGithubAttestationImpl(bctx, a1, a2, s)
			})
		},
	})
}

func (p *Policy) builtinGithubAttestationImpl(bctx rego.BuiltinContext, a1, a2 *ast.Term, s *state) (*ast.Term, error) {
	inp := s.Input
	if inp.HTTP == nil {
		return nil, nil
	}

	obja, ok := a1.Value.(ast.Object)
	if !ok {
		return nil, errors.Errorf("%s: expected object, got %T", funcGithubAttestation, a1.Value)
	}

	httpValue, err := ast.InterfaceToValue(inp.HTTP)
	if err != nil {
		return nil, errors.Wrapf(err, "%s: failed converting object to interface", funcGithubAttestation)
	}

	if obja.Compare(httpValue) != 0 {
		return nil, errors.Errorf("%s: first argument is not the same as input http", funcGithubAttestation)
	}

	repo, ok := a2.Value.(ast.String)
	if !ok {
		return nil, errors.Errorf("%s: expected repository name string, got %T", funcGithubAttestation, a2.Value)
	}

	if inp.HTTP.Checksum == "" {
		s.addUnknown(funcGithubAttestation)
		return nil, nil
	}

	if p.opt.SourceResolver == nil {
		return nil, errors.Errorf("%s: source resolver is not configured", funcGithubAttestation)
	}
	if p.opt.VerifierProvider == nil {
		return nil, errors.Errorf("%s: policy verifier is not configured", funcGithubAttestation)
	}

	dgst, err := digest.Parse(inp.HTTP.Checksum)
	if err != nil {
		return nil, errors.Wrapf(err, "%s: invalid checksum", funcGithubAttestation)
	}

	v, err := p.opt.VerifierProvider()
	if err != nil {
		return nil, errors.Wrapf(err, "%s: getting policy verifier", funcGithubAttestation)
	}

	bundles, err := p.readGitHubAttestationBundles(bctx.Context, string(repo), dgst)
	if err != nil {
		p.log(logrus.InfoLevel, "%s: failed reading bundles for %s@%s: %v", funcGithubAttestation, repo, dgst, err)
		return nil, nil
	}
	if len(bundles) == 0 {
		p.log(logrus.InfoLevel, "%s: no bundle found for %s@%s", funcGithubAttestation, repo, dgst)
		return nil, nil
	}

	for _, bundleBytes := range bundles {
		siRaw, err := v.VerifyArtifact(bctx.Context, dgst, bundleBytes)
		if err != nil {
			p.log(logrus.InfoLevel, "%s: failed verifying bundle for %s@%s: %v", funcGithubAttestation, repo, dgst, err)
			continue
		}
		si := toAttestationSignature(siRaw)
		astVal, err := ast.InterfaceToValue(si)
		if err != nil {
			return nil, errors.Wrapf(err, "%s: failed converting verification result", funcGithubAttestation)
		}
		return ast.NewTerm(astVal), nil
	}

	return nil, nil
}

func (p *Policy) readGitHubAttestationBundles(ctx context.Context, repo string, dgst digest.Digest) ([][]byte, error) {
	const (
		attestationFilename = "attestation.json"
		bundleFilename      = "bundle.json"
	)

	u := fmt.Sprintf(
		"https://api.github.com/repos/%s/attestations/%s?predicate_type=%s",
		repo,
		dgst.String(),
		url.QueryEscape(slsa1.PredicateSLSAProvenance),
	)
	st := llb.HTTP(
		u,
		llb.Filename(attestationFilename),
		llb.WithCustomNamef("[policy] fetch GitHub attestation %s@%s", repo, dgst.String()),
		llb.Header(llb.HTTPHeader{
			Accept: "application/vnd.github+json",
		}),
	)
	ref, err := p.opt.SourceResolver.ResolveState(ctx, st)
	if err != nil {
		return nil, errors.Wrapf(err, "resolve GitHub attestation request")
	}

	raw, err := ref.ReadFile(ctx, gwclient.ReadRequest{Filename: attestationFilename})
	if err != nil {
		return nil, errors.Wrapf(err, "read GitHub attestation response")
	}

	bundles, bundleURLs := githubAttestationBundlesFromResponse(raw)
	p.log(logrus.InfoLevel, "%s: fetched %d inline bundles and %d bundle URLs from %s", funcGithubAttestation, len(bundles), len(bundleURLs), u)
	for _, bu := range bundleURLs {
		// bundle_url is a signed URL to the bundle payload.
		st := llb.HTTP(
			bu,
			llb.Filename(bundleFilename),
			llb.WithCustomNamef("[policy] fetch GitHub attestation bundle %s", stripRawQuery(bu)),
		)
		ref, err := p.opt.SourceResolver.ResolveState(ctx, st)
		if err != nil {
			p.log(logrus.InfoLevel, "%s: failed fetching bundle_url %s: %v", funcGithubAttestation, bu, err)
			continue
		}
		bundleRaw, err := ref.ReadFile(ctx, gwclient.ReadRequest{Filename: bundleFilename})
		if err != nil {
			p.log(logrus.InfoLevel, "%s: failed reading bundle_url %s: %v", funcGithubAttestation, bu, err)
			continue
		}
		bundleRaw = bytes.TrimSpace(bundleRaw)
		if len(bundleRaw) == 0 || bytes.Equal(bundleRaw, []byte("null")) {
			continue
		}
		if shouldDecodeSnappyBundleURL(bu) {
			decoded, err := snappy.Decode(nil, bundleRaw)
			if err != nil {
				p.log(logrus.InfoLevel, "%s: failed decoding snappy bundle_url %s: %v", funcGithubAttestation, bu, err)
				continue
			}
			bundleRaw = decoded
		}
		bundles = append(bundles, bundleRaw)
	}
	return bundles, nil
}

func githubAttestationBundlesFromResponse(raw []byte) ([][]byte, []string) {
	t := bytes.TrimSpace(raw)
	if len(t) == 0 {
		return nil, nil
	}

	var parsed struct {
		Attestations []struct {
			Bundle    json.RawMessage `json:"bundle"`
			BundleURL string          `json:"bundle_url"`
		} `json:"attestations"`
	}
	if err := json.Unmarshal(t, &parsed); err != nil {
		return nil, nil
	}
	out := make([][]byte, 0, len(parsed.Attestations))
	bundleURLs := make([]string, 0, len(parsed.Attestations))
	appendCandidate := func(dt []byte) {
		dt = bytes.TrimSpace(dt)
		if len(dt) == 0 || bytes.Equal(dt, []byte("null")) {
			return
		}
		out = append(out, dt)
	}
	for _, a := range parsed.Attestations {
		appendCandidate(a.Bundle)
		if a.BundleURL != "" {
			bundleURLs = append(bundleURLs, a.BundleURL)
		}
	}
	return out, bundleURLs
}

func shouldDecodeSnappyBundleURL(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	return strings.HasSuffix(u.Path, ".json.sn")
}

func stripRawQuery(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	u.RawQuery = ""
	return u.String()
}

func (p *Policy) builtinArtifactAttestationImpl(bctx rego.BuiltinContext, a1, a2 *ast.Term, s *state) (*ast.Term, error) {
	inp := s.Input
	if inp.HTTP == nil {
		return nil, nil
	}

	obja, ok := a1.Value.(ast.Object)
	if !ok {
		return nil, errors.Errorf("%s: expected object, got %T", funcArtifactAttestation, a1.Value)
	}

	httpValue, err := ast.InterfaceToValue(inp.HTTP)
	if err != nil {
		return nil, errors.Wrapf(err, "%s: failed converting object to interface", funcArtifactAttestation)
	}

	if obja.Compare(httpValue) != 0 {
		return nil, errors.Errorf("%s: first argument is not the same as input http", funcArtifactAttestation)
	}

	path, ok := a2.Value.(ast.String)
	if !ok {
		return nil, errors.Errorf("%s: expected string path, got %T", funcArtifactAttestation, a2.Value)
	}

	if inp.HTTP.Checksum == "" {
		s.addUnknown(funcArtifactAttestation)
		return nil, nil
	}

	dgst, err := digest.Parse(inp.HTTP.Checksum)
	if err != nil {
		return nil, errors.Wrapf(err, "%s: invalid checksum", funcArtifactAttestation)
	}

	bundleBytes, err := p.readFile(string(path), 8*1024*1024)
	if err != nil {
		return nil, err
	}

	if p.opt.VerifierProvider == nil {
		return nil, errors.Errorf("%s: policy verifier is not configured", funcArtifactAttestation)
	}
	v, err := p.opt.VerifierProvider()
	if err != nil {
		return nil, errors.Wrapf(err, "%s: getting policy verifier", funcArtifactAttestation)
	}

	siRaw, err := v.VerifyArtifact(bctx.Context, dgst, bundleBytes)
	if err != nil {
		return nil, nil
	}

	si := toAttestationSignature(siRaw)
	astVal, err := ast.InterfaceToValue(si)
	if err != nil {
		return nil, errors.Wrapf(err, "%s: failed converting verification result", funcArtifactAttestation)
	}

	return ast.NewTerm(astVal), nil
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

func (p *Policy) builtinVerifyHTTPPGPSignatureImpl(_ rego.BuiltinContext, a1, a2, a3 *ast.Term, s *state) (*ast.Term, error) {
	inp := s.Input
	if inp.HTTP == nil {
		return ast.BooleanTerm(false), nil
	}

	obja, ok := a1.Value.(ast.Object)
	if !ok {
		return nil, errors.Errorf("%s: expected object, got %T", funcVerifyHTTPPGPSignature, a1.Value)
	}

	httpValue, err := ast.InterfaceToValue(inp.HTTP)
	if err != nil {
		return nil, errors.Wrapf(err, "%s: failed converting object to interface", funcVerifyHTTPPGPSignature)
	}

	if obja.Compare(httpValue) != 0 {
		return nil, errors.Errorf("%s: first argument is not the same as input http", funcVerifyHTTPPGPSignature)
	}

	sigPath, ok := a2.Value.(ast.String)
	if !ok {
		return nil, errors.Errorf("%s: expected string signature path, got %T", funcVerifyHTTPPGPSignature, a2.Value)
	}
	pubKeyPath, ok := a3.Value.(ast.String)
	if !ok {
		return nil, errors.Errorf("%s: expected string pubkey path, got %T", funcVerifyHTTPPGPSignature, a3.Value)
	}

	signatureData, err := p.readFile(string(sigPath), 512*1024)
	if err != nil {
		return nil, err
	}
	pubKeyData, err := p.readFile(string(pubKeyPath), 512*1024)
	if err != nil {
		return nil, err
	}

	sig, _, err := pgpsign.ParseArmoredDetachedSignature(signatureData)
	if err != nil {
		return nil, errors.Wrapf(err, "%s: failed to parse detached signature", funcVerifyHTTPPGPSignature)
	}
	keyring, err := pgpsign.ReadAllArmoredKeyRings(pubKeyData)
	if err != nil {
		return nil, errors.Wrapf(err, "%s: failed to read armored keyring", funcVerifyHTTPPGPSignature)
	}

	algo, err := toPBChecksumAlgo(sig.Hash)
	if err != nil {
		return nil, errors.Wrapf(err, "%s: unsupported signature hash", funcVerifyHTTPPGPSignature)
	}
	suffix := slices.Clone(sig.HashSuffix)
	checksumReq := &gwpb.ChecksumRequest{Algo: algo, Suffix: suffix}

	resp := inp.HTTP.checksumResponseForSignature
	if resp == nil || resp.Digest == "" {
		s.checksumNeededForSignature = checksumReq
		s.addUnknown(funcVerifyHTTPPGPSignature)
		return ast.BooleanTerm(false), nil
	}
	if !bytes.Equal(resp.Suffix, suffix) {
		s.checksumNeededForSignature = checksumReq
		s.addUnknown(funcVerifyHTTPPGPSignature)
		return ast.BooleanTerm(false), nil
	}
	dgst, err := digest.Parse(resp.Digest)
	if err != nil {
		return nil, errors.Wrapf(err, "%s: invalid checksum digest", funcVerifyHTTPPGPSignature)
	}
	if !checksumAlgoMatches(algo, dgst.Algorithm()) {
		s.checksumNeededForSignature = checksumReq
		s.addUnknown(funcVerifyHTTPPGPSignature)
		return ast.BooleanTerm(false), nil
	}

	if err := pgpsign.VerifySignatureWithDigest(sig, keyring, dgst); err != nil {
		return ast.BooleanTerm(false), nil
	}
	return ast.BooleanTerm(true), nil
}

func toPBChecksumAlgo(hash crypto.Hash) (gwpb.ChecksumRequest_ChecksumAlgo, error) {
	switch hash {
	case crypto.SHA256:
		return gwpb.ChecksumRequest_CHECKSUM_ALGO_SHA256, nil
	case crypto.SHA384:
		return gwpb.ChecksumRequest_CHECKSUM_ALGO_SHA384, nil
	case crypto.SHA512:
		return gwpb.ChecksumRequest_CHECKSUM_ALGO_SHA512, nil
	default:
		return gwpb.ChecksumRequest_CHECKSUM_ALGO_SHA256, errors.Errorf("unsupported signature hash algorithm %v", hash)
	}
}

func checksumAlgoMatches(algo gwpb.ChecksumRequest_ChecksumAlgo, digestAlgo digest.Algorithm) bool {
	switch algo {
	case gwpb.ChecksumRequest_CHECKSUM_ALGO_SHA256:
		return digestAlgo == digest.SHA256
	case gwpb.ChecksumRequest_CHECKSUM_ALGO_SHA384:
		return digestAlgo == digest.SHA384
	case gwpb.ChecksumRequest_CHECKSUM_ALGO_SHA512:
		return digestAlgo == digest.SHA512
	default:
		return false
	}
}

func (p *Policy) readFile(path string, limit int64) ([]byte, error) {
	if p.opt.FS == nil {
		return nil, errors.Errorf("no policy FS defined for reading context files")
	}
	root, closeFS, err := p.opt.FS()
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get policy FS for reading context files")
	}
	if closeFS != nil {
		defer closeFS()
	}

	f, err := root.Open(path)
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
