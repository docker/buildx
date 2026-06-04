package replay

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/containerd/platforms"
	"github.com/docker/buildx/policy"
	"github.com/distribution/reference"
	slsa1 "github.com/in-toto/in-toto-golang/in_toto/slsa_provenance/v1"
	gwpb "github.com/moby/buildkit/frontend/gateway/pb"
	spb "github.com/moby/buildkit/sourcepolicy/pb"
	"github.com/moby/buildkit/sourcepolicy/policysession"
	"github.com/moby/buildkit/util/purl"
	"github.com/opencontainers/go-digest"
	ocispecs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/pkg/errors"
)

// PinIndex is a resolved, URI/digest-keyed view of the predicate's
// ResolvedDependencies suitable for fast policy-callback lookup.
//
// Two lookup tables are maintained: byURI maps the material's URI (e.g.
// "docker-image://alpine:3.18" or "https://example.com/foo.tar") to the pinned
// digest, and byDigest maps an already-pinned digest to the URI it belongs to.
// Either side of a pin index entry is sufficient to match a source-policy
// request; when both are present on the request they must refer to the same
// pin entry.
type PinIndex struct {
	byURI     map[string]digest.Digest
	byDigest  map[digest.Digest]string
	materials []string
}

// NewPinIndex builds a PinIndex from the predicate's ResolvedDependencies.
// When a material has multiple digest entries (e.g. sha256 + sha512), the
// sha256 entry is preferred; otherwise the first entry wins. Materials without
// a usable digest are skipped.
func NewPinIndex(p *Predicate) *PinIndex {
	idx := &PinIndex{
		byURI:    map[string]digest.Digest{},
		byDigest: map[digest.Digest]string{},
	}
	if p == nil {
		return idx
	}
	seenMaterials := map[string]struct{}{}
	for _, m := range p.ResolvedDependencies() {
		if disp := formatPinMaterial(m); disp != "" {
			if _, ok := seenMaterials[disp]; !ok {
				seenMaterials[disp] = struct{}{}
				idx.materials = append(idx.materials, disp)
			}
		}
		d := preferredDigest(m.Digest)
		if d == "" {
			continue
		}
		if m.URI != "" {
			idx.byURI[m.URI] = d
			if canon, ok := canonicalMaterialIdentifier(m); ok {
				idx.byURI[canon] = d
				for _, alias := range canonicalIdentifierAliases(canon) {
					idx.byURI[alias] = d
				}
			}
		}
		idx.byDigest[d] = m.URI
	}
	sort.Strings(idx.materials)
	return idx
}

// preferredDigest picks a single digest from an in-toto DigestSet. sha256 is
// preferred so it aligns with how BuildKit's source-meta responses return
// image and http digests. Any other algorithm is accepted as a fallback.
func preferredDigest(set map[string]string) digest.Digest {
	if set == nil {
		return ""
	}
	if v, ok := set["sha256"]; ok && v != "" {
		return digest.NewDigestFromEncoded(digest.SHA256, v)
	}
	for alg, v := range set {
		if v == "" {
			continue
		}
		return digest.NewDigestFromEncoded(digest.Algorithm(alg), v)
	}
	return ""
}

// Len reports the number of pin entries. Used by `--dry-run`.
func (p *PinIndex) Len() int {
	if p == nil {
		return 0
	}
	return len(p.byDigest)
}

// Lookup resolves a URI to its pinned digest. Returns ("", false) when the
// URI is not covered by the index.
func (p *PinIndex) Lookup(uri string) (digest.Digest, bool) {
	if p == nil {
		return "", false
	}
	d, ok := p.byURI[uri]
	return d, ok
}

// ReplayPinCallback returns a policysession.PolicyCallback that enforces the
// pin index. Sources covered by the index are ALLOWed when their requested
// digest matches; unknown sources are DENY (fail-closed); covered sources
// with wrong digest are DENY with a DenyMessage.
func ReplayPinCallback(idx *PinIndex) policysession.PolicyCallback {
	return func(ctx context.Context, req *policysession.CheckPolicyRequest) (*policysession.DecisionResponse, *gwpb.ResolveSourceMetaRequest, error) {
		uri, observed := extractSourceIdentity(req)
		if uri == "" && observed == "" {
			return denyResponse("replay pin: request carried no source identifier"), nil, nil
		}

		// URI-matched: compare observed digest against the pinned digest.
		if idx != nil && uri != "" {
			if pinned, ok := idx.byURI[uri]; ok {
				if decision, handled, err := convertPinnedImage(req, pinned); err != nil {
					return nil, nil, err
				} else if handled {
					return decision, nil, nil
				}
				if observed == "" {
					return allowResponse(), nil, nil
				}
				if pinned == observed {
					return allowResponse(), nil, nil
				}
				return denyResponse(fmt.Sprintf("replay pin mismatch for %s: expected %s, got %s", uri, pinned, observed)), nil, nil
			}
		}

		// Digest-only match: the observed digest matches a pinned material
		// whose URI was not available on the request. Accept this — the
		// content is the pinned bytes regardless of how the frontend named
		// them.
		if idx != nil && observed != "" {
			if _, ok := idx.byDigest[observed]; ok {
				return allowResponse(), nil, nil
			}
		}

		// Source is not covered by the pin index. Fail closed.
		return denyResponse(notCoveredMessage(idx, req, displaySource(uri, observed))), nil, nil
	}
}

func convertPinnedImage(req *policysession.CheckPolicyRequest, pinned digest.Digest) (*policysession.DecisionResponse, bool, error) {
	if req == nil || req.Source == nil || req.Source.GetSource() == nil {
		return nil, false, nil
	}
	src := req.Source.GetSource()
	if canon := canonicalRequestSource(req); canon != "" && canon != src.Identifier {
		clone := *src
		clone.Identifier = canon
		src = &clone
	}
	if !strings.HasPrefix(src.Identifier, "docker-image://") {
		return nil, false, nil
	}
	newSrc, err := policy.AddPinToImage(src, pinned)
	if err != nil {
		return nil, false, errors.Wrap(err, "failed to pin covered image source")
	}
	if newSrc.Identifier == src.Identifier {
		return allowResponse(), true, nil
	}
	return &policysession.DecisionResponse{
		Action: spb.PolicyAction_CONVERT,
		Update: newSrc,
	}, true, nil
}

// extractSourceIdentity pulls the URI and the digest (if known on this call)
// from a CheckPolicyRequest. Supports the three source shapes BuildKit emits
// on a session policy request: image, http, git.
func extractSourceIdentity(req *policysession.CheckPolicyRequest) (uri string, dgst digest.Digest) {
	if req == nil || req.Source == nil {
		return "", ""
	}
	src := req.Source.GetSource()
	if src != nil {
		uri = src.GetIdentifier()
	}

	// Image response carries the resolved digest once BuildKit has asked the
	// registry. First invocation (before resolution) comes without a digest —
	// that is the expected path for the ALLOW-pending branch.
	if img := req.Source.GetImage(); img != nil {
		if v := img.GetDigest(); v != "" {
			dgst = digest.Digest(v)
		}
	}
	if h := req.Source.GetHTTP(); h != nil {
		if v := h.GetChecksum(); v != "" {
			dgst = digest.Digest(v)
		}
	}
	if g := req.Source.GetGit(); g != nil {
		// Git materials do not carry a sha256 content digest. Treat the
		// commit checksum as a placeholder — the URI match is the real
		// enforcement point for git.
		if v := g.GetCommitChecksum(); v != "" && dgst == "" {
			dgst = digest.Digest(v)
		}
	}
	if uri != "" {
		uri, dgst = normalizeRequestSourceIdentity(uri, dgst)
	}
	return uri, dgst
}

func displaySource(uri string, dgst digest.Digest) string {
	switch {
	case uri != "" && dgst != "":
		return fmt.Sprintf("%s@%s", uri, dgst)
	case uri != "":
		return uri
	case dgst != "":
		return dgst.String()
	default:
		return "<unknown>"
	}
}

func canonicalMaterialIdentifier(m slsa1.ResourceDescriptor) (string, bool) {
	src, _, err := policy.ParseSLSAMaterial(m)
	if err != nil || src == nil || src.Identifier == "" {
		return "", false
	}
	return src.Identifier, true
}

func canonicalIdentifierAliases(id string) []string {
	refStr, ok := strings.CutPrefix(id, "docker-image://")
	if !ok {
		return nil
	}
	if refBase, _, ok := strings.Cut(refStr, "@"); ok {
		return []string{"docker-image://" + refBase}
	}
	if _, err := reference.ParseNormalizedNamed(refStr); err != nil {
		return nil
	}
	return nil
}

func normalizeRequestSourceIdentity(uri string, dgst digest.Digest) (string, digest.Digest) {
	refStr, ok := strings.CutPrefix(uri, "docker-image://")
	if !ok {
		return uri, dgst
	}
	if refBase, refDigest, ok := strings.Cut(refStr, "@"); ok {
		if dgst == "" {
			if parsed, err := digest.Parse(refDigest); err == nil {
				dgst = parsed
			}
		}
		return "docker-image://" + refBase, dgst
	}
	return uri, dgst
}

func notCoveredMessage(idx *PinIndex, req *policysession.CheckPolicyRequest, src string) string {
	if idx == nil || len(idx.materials) == 0 {
		return fmt.Sprintf("replay pin: no provenance material matched requested source\n  target: %s", formatTargetSource(req, src))
	}
	var b strings.Builder
	b.WriteString("replay pin: no provenance material matched requested source")
	b.WriteString("\n  target: ")
	b.WriteString(formatTargetSource(req, src))
	b.WriteString("\n  provenance materials:")
	for _, m := range idx.materials {
		b.WriteString("\n  - ")
		b.WriteString(m)
	}
	return b.String()
}

func requestPlatform(req *policysession.CheckPolicyRequest) *ocispecs.Platform {
	if req == nil || req.Platform == nil {
		return nil
	}
	p := ocispecs.Platform{
		OS:           req.Platform.OS,
		Architecture: req.Platform.Architecture,
		Variant:      req.Platform.Variant,
	}
	norm := platforms.Normalize(p)
	return &norm
}

func formatPinMaterial(m slsa1.ResourceDescriptor) string {
	if m.URI == "" {
		return ""
	}
	src, p, err := policy.ParseSLSAMaterial(m)
	switch {
	case err != nil || src == nil || src.Identifier == "":
		return m.URI
	case p != nil:
		return fmt.Sprintf("uri=%s canonical=%s platform=%s", m.URI, src.Identifier, platforms.Format(*p))
	case src.Identifier != m.URI:
		return fmt.Sprintf("uri=%s canonical=%s", m.URI, src.Identifier)
	default:
		return "uri=" + m.URI
	}
}

func formatTargetSource(req *policysession.CheckPolicyRequest, src string) string {
	parts := []string{"uri=" + src}
	if canon := canonicalRequestSource(req); canon != "" && canon != src {
		parts = append(parts, "canonical="+canon)
	}
	if p := requestPlatform(req); p != nil {
		parts = append(parts, "platform="+platforms.Format(*p))
	}
	return strings.Join(parts, " ")
}

func canonicalRequestSource(req *policysession.CheckPolicyRequest) string {
	if req == nil || req.Source == nil || req.Source.Source == nil {
		return ""
	}
	uri := req.Source.Source.Identifier
	if uri == "" {
		return ""
	}
	if strings.HasPrefix(uri, "pkg:docker/") {
		refStr, _, err := purl.PURLToRef(uri)
		if err != nil {
			return ""
		}
		named, err := reference.ParseNormalizedNamed(refStr)
		if err != nil {
			return ""
		}
		return "docker-image://" + named.String()
	}
	return uri
}

func allowResponse() *policysession.DecisionResponse {
	return &policysession.DecisionResponse{Action: spb.PolicyAction_ALLOW}
}

func denyResponse(msg string) *policysession.DecisionResponse {
	return &policysession.DecisionResponse{
		Action:       spb.PolicyAction_DENY,
		DenyMessages: []*policysession.DenyMessage{{Message: msg}},
	}
}

// ComposeCallbacks aggregates any number of callbacks via the
// MultiPolicyCallback helper so a replay pin callback composes cleanly with
// any future overlay. The replay callback MUST be the last (most-strict)
// entry; callers are expected to append it last.
func ComposeCallbacks(cbs ...policysession.PolicyCallback) policysession.PolicyCallback {
	return policy.MultiPolicyCallback(cbs...)
}
