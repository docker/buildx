package policy

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"sync"

	"github.com/containerd/containerd/v2/core/content"
	"github.com/containerd/containerd/v2/core/remotes"
	cerrderfs "github.com/containerd/errdefs"
	"github.com/docker/buildx/util/confutil"
	gwpb "github.com/moby/buildkit/frontend/gateway/pb"
	policyverifier "github.com/moby/policy-helpers"
	policyimage "github.com/moby/policy-helpers/image"
	"github.com/opencontainers/go-digest"
	ocispecs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/pkg/errors"
)

type PolicyVerifierProvider func() (*policyverifier.Verifier, error)

func SignatureVerifier(cfg *confutil.Config) PolicyVerifierProvider {
	if cfg == nil {
		return nil
	}
	var (
		mu sync.Mutex
		v  *policyverifier.Verifier
	)
	return func() (*policyverifier.Verifier, error) {
		mu.Lock()
		defer mu.Unlock()

		if v != nil {
			return v, nil
		}

		root := cfg.Dir()
		confDir := filepath.Join(root, "policy")
		if err := cfg.MkdirAll("policy/tuf", 0o755); err != nil {
			return nil, errors.Wrapf(err, "failed to create policy verifier config dir")
		}

		nv, err := policyverifier.NewVerifier(policyverifier.Config{
			StateDir: confDir,
		})
		if err != nil {
			return nil, errors.Wrapf(err, "failed to create policy verifier")
		}
		v = nv
		return v, nil
	}
}

func parseSignatures(ctx context.Context, getVerifier PolicyVerifierProvider, ac *gwpb.AttestationChain, platform *ocispecs.Platform) ([]AttestationSignature, error) {
	if ac.Root == "" || ac.AttestationManifest == "" || len(ac.SignatureManifests) == 0 {
		return nil, nil
	}

	root, err := digest.Parse(ac.Root)
	if err != nil {
		return nil, err
	}

	att, err := digest.Parse(ac.AttestationManifest)
	if err != nil {
		return nil, err
	}

	sigs := make([]digest.Digest, 0, len(ac.SignatureManifests))
	for _, sm := range ac.SignatureManifests {
		d, err := digest.Parse(sm)
		if err != nil {
			return nil, err
		}
		sigs = append(sigs, d)
	}
	acp := &acProvider{
		blobs:      ac.Blobs,
		signatures: sigs,
		att:        att,
	}

	rootBlob, ok := ac.Blobs[root.String()]
	if !ok {
		return nil, errors.Errorf("root blob %s not found", root)
	}
	desc := toOCIDescriptor(rootBlob.Descriptor_)

	if desc.MediaType != ocispecs.MediaTypeImageIndex {
		return nil, nil
	}

	sc, err := policyimage.ResolveSignatureChain(ctx, acp, desc, platform)
	if err != nil {
		return nil, errors.Wrapf(err, "resolving signature chain for image %s", desc.Digest)
	}

	if sc.AttestationManifest == nil || sc.SignatureManifest == nil {
		return nil, nil
	}

	if sc.AttestationManifest.Digest != att {
		return nil, errors.Errorf("attestation manifest digest mismatch: expected %s, got %s", att, sc.AttestationManifest.Digest)
	}

	if getVerifier == nil {
		return nil, errors.New("policy verifier is not configured")
	}
	v, err := getVerifier()
	if err != nil {
		return nil, errors.Wrapf(err, "getting policy verifier")
	}

	siRaw, err := v.VerifyImage(ctx, acp, desc, platform)
	if err != nil {
		return nil, errors.Wrapf(err, "verifying image signatures")
	}

	si := AttestationSignature{
		raw:             siRaw,
		Timestamps:      siRaw.Timestamps,
		IsDHI:           siRaw.IsDHI,
		DockerReference: siRaw.DockerReference,
		SignatureType:   toSignatureType(siRaw.SignatureType),
		SignatureKind:   toSignatureKind(siRaw.Kind),
	}

	// TODO: signature type after upstream update

	if siRaw.Signer != nil {
		si.Signer = &SignerInfo{
			CertificateIssuer:                   siRaw.Signer.CertificateIssuer,
			SubjectAlternativeName:              siRaw.Signer.SubjectAlternativeName,
			Issuer:                              siRaw.Signer.Issuer,
			BuildSignerURI:                      siRaw.Signer.BuildSignerURI,
			BuildSignerDigest:                   siRaw.Signer.BuildSignerDigest,
			RunnerEnvironment:                   siRaw.Signer.RunnerEnvironment,
			SourceRepositoryURI:                 siRaw.Signer.SourceRepositoryURI,
			SourceRepositoryDigest:              siRaw.Signer.SourceRepositoryDigest,
			SourceRepositoryRef:                 siRaw.Signer.SourceRepositoryRef,
			SourceRepositoryIdentifier:          siRaw.Signer.SourceRepositoryIdentifier,
			SourceRepositoryOwnerURI:            siRaw.Signer.SourceRepositoryOwnerURI,
			SourceRepositoryOwnerIdentifier:     siRaw.Signer.SourceRepositoryOwnerIdentifier,
			BuildConfigURI:                      siRaw.Signer.BuildConfigURI,
			BuildConfigDigest:                   siRaw.Signer.BuildConfigDigest,
			BuildTrigger:                        siRaw.Signer.BuildTrigger,
			RunInvocationURI:                    siRaw.Signer.RunInvocationURI,
			SourceRepositoryVisibilityAtSigning: siRaw.Signer.SourceRepositoryVisibilityAtSigning,
		}
	}

	return []AttestationSignature{si}, nil
}

type acProvider struct {
	blobs      map[string]*gwpb.Blob
	signatures []digest.Digest
	att        digest.Digest
}

var _ policyimage.ReferrersProvider = &acProvider{}

func (p *acProvider) FetchReferrers(ctx context.Context, dgst digest.Digest, opts ...remotes.FetchReferrersOpt) ([]ocispecs.Descriptor, error) {
	if dgst != p.att {
		return nil, nil
	}
	out := make([]ocispecs.Descriptor, 0, len(p.signatures))
	for _, d := range p.signatures {
		b, ok := p.blobs[d.String()]
		if !ok {
			continue
		}
		desc := toOCIDescriptor(b.Descriptor_)

		var mfst ocispecs.Manifest
		if err := json.Unmarshal(b.Data, &mfst); err != nil {
			return nil, errors.Wrapf(err, "unmarshal signature manifest %s", d)
		}
		desc.ArtifactType = mfst.ArtifactType

		// on image manifest assume legacy format
		if desc.ArtifactType == "" {
			desc.ArtifactType = policyimage.ArtifactTypeCosignSignature
		}
		out = append(out, desc)
	}
	return out, nil
}

func (p *acProvider) ReaderAt(ctx context.Context, desc ocispecs.Descriptor) (content.ReaderAt, error) {
	b, ok := p.blobs[desc.Digest.String()]
	if !ok {
		return nil, errors.WithStack(cerrderfs.ErrNotFound)
	}
	return &readerAt{buf: bytes.NewReader(b.Data)}, nil
}

type readerAt struct {
	buf *bytes.Reader
}

var _ content.ReaderAt = &readerAt{}

func (r *readerAt) ReadAt(p []byte, off int64) (n int, err error) {
	return r.buf.ReadAt(p, off)
}

func (r *readerAt) Size() int64 {
	return int64(r.buf.Len())
}

func (r *readerAt) Close() error {
	return nil
}

func toOCIDescriptor(d *gwpb.Descriptor) ocispecs.Descriptor {
	return ocispecs.Descriptor{
		MediaType: d.MediaType,
		Digest:    digest.Digest(d.Digest),
		Size:      d.Size,
	}
}
