package replay

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/containerd/platforms"
	"github.com/distribution/reference"
	"github.com/docker/buildx/policy"
	slsa1 "github.com/in-toto/in-toto-golang/in_toto/slsa_provenance/v1"
	gwpb "github.com/moby/buildkit/frontend/gateway/pb"
	solverpb "github.com/moby/buildkit/solver/pb"
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
// Exact material identifiers and lossy aliases are kept separate. An alias can
// name more than one pin (for example the same image tag at two digests), so it
// must never use last-write-wins selection.
type PinIndex struct {
	byURI     map[string][]sourcePin
	byAlias   map[string][]sourcePin
	byDigest  map[digest.Digest]struct{}
	materials []string
}

type sourcePin struct {
	digest   digest.Digest
	platform *ocispecs.Platform
}

// NewPinIndex builds a PinIndex from the predicate's ResolvedDependencies.
// When a material has multiple digest entries (e.g. sha256 + sha512), the
// sha256 entry is preferred; otherwise the first entry wins. Materials without
// a usable digest are skipped.
func NewPinIndex(p *Predicate) *PinIndex {
	idx := &PinIndex{
		byURI:    map[string][]sourcePin{},
		byAlias:  map[string][]sourcePin{},
		byDigest: map[digest.Digest]struct{}{},
	}
	if p == nil {
		return idx
	}
	materials := p.ResolvedDependencies()
	if cfg := p.ConfigSource(); cfg.URI != "" && len(cfg.Digest) > 0 {
		materials = append(materials, slsa1.ResourceDescriptor{
			URI:    cfg.URI,
			Digest: cfg.Digest,
		})
	}
	seenMaterials := map[string]struct{}{}
	for _, m := range materials {
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
		pin := sourcePin{digest: d}
		if m.URI != "" {
			if canon, platform, ok := canonicalMaterialIdentifier(m); ok {
				pin.platform = platform
				idx.byURI[canon] = appendPin(idx.byURI[canon], pin)
				for _, alias := range canonicalIdentifierAliases(canon) {
					idx.byAlias[alias] = appendPin(idx.byAlias[alias], pin)
				}
			}
			idx.byURI[m.URI] = appendPin(idx.byURI[m.URI], pin)
		}
		idx.byDigest[d] = struct{}{}
	}
	sort.Strings(idx.materials)
	return idx
}

// preferredDigest picks a single digest from an in-toto DigestSet. sha256 is
// preferred so it aligns with how BuildKit's source-meta responses return
// image and http digests. Other algorithms are considered in lexical order
// so provenance with multiple non-sha256 entries is deterministic.
func preferredDigest(set map[string]string) digest.Digest {
	if set == nil {
		return ""
	}
	if v, ok := set["sha256"]; ok && v != "" {
		return digest.NewDigestFromEncoded(digest.SHA256, v)
	}
	algorithms := make([]string, 0, len(set))
	for alg, v := range set {
		if v == "" {
			continue
		}
		algorithms = append(algorithms, alg)
	}
	sort.Strings(algorithms)
	for _, alg := range algorithms {
		return digest.NewDigestFromEncoded(digest.Algorithm(alg), set[alg])
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
	if pins, ok := p.byURI[uri]; ok {
		return uniquePinDigest(pins)
	}
	return uniquePinDigest(p.byAlias[uri])
}

func appendPin(pins []sourcePin, pin sourcePin) []sourcePin {
	for _, existing := range pins {
		if existing.digest == pin.digest && platformEqual(existing.platform, pin.platform) {
			return pins
		}
	}
	return append(pins, pin)
}

func platformEqual(a, b *ocispecs.Platform) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return platforms.OnlyStrict(platforms.Normalize(*a)).Match(platforms.Normalize(*b))
}

func uniquePinDigest(pins []sourcePin) (digest.Digest, bool) {
	var found digest.Digest
	for _, pin := range pins {
		if found == "" {
			found = pin.digest
			continue
		}
		if found != pin.digest {
			return "", false
		}
	}
	return found, found != ""
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

		// URI-matched: rewrite the source operation so BuildKit's source
		// implementation enforces the recorded pin. Metadata is not present on
		// BuildKit's initial policy request, so allowing a covered URI before
		// adding these attributes would leave HTTP and Git sources unpinned.
		if idx != nil && uri != "" {
			if pinned, covered, reason := idx.resolve(req, uri, observed); covered {
				if reason != "" {
					return denyResponse(reason), nil, nil
				}
				decision, supported, err := convertPinnedSource(req, pinned)
				if err != nil {
					return nil, nil, err
				} else if decision != nil {
					return decision, nil, nil
				}
				if observed == "" {
					if supported {
						return allowResponse(), nil, nil
					}
					return denyResponse(fmt.Sprintf("replay pin: cannot enforce recorded digest for %s", uri)), nil, nil
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

func (p *PinIndex) resolve(req *policysession.CheckPolicyRequest, uri string, observed digest.Digest) (digest.Digest, bool, string) {
	keys := []string{uri}
	if canon := canonicalRequestSource(req); canon != "" && canon != uri {
		keys = append(keys, canon)
	}
	for _, key := range keys {
		if pins, ok := p.byURI[key]; ok {
			return selectPin(pins, requestPlatform(req), observed, key)
		}
	}
	for _, key := range keys {
		aliases := append([]string{key}, canonicalIdentifierAliases(key)...)
		for _, alias := range aliases {
			if pins, ok := p.byAlias[alias]; ok {
				return selectPin(pins, requestPlatform(req), observed, alias)
			}
		}
	}
	return "", false, ""
}

func selectPin(pins []sourcePin, platform *ocispecs.Platform, observed digest.Digest, source string) (digest.Digest, bool, string) {
	// A platform-qualified material still pins the same immutable image index.
	// If every matching provenance entry agrees on that digest, the request
	// platform cannot make the pin ambiguous and must not exclude it. This is
	// important when replaying on a builder whose BUILDPLATFORM differs from
	// the builder that produced the provenance.
	if pinned, ok := uniquePinDigest(pins); ok {
		return pinned, true, ""
	}

	// Platform is only a tie-breaker when the source name has multiple recorded
	// digests. In that case it prevents silently selecting another platform's
	// pin while retaining the fail-closed behavior for unknown platforms.
	candidates := pins
	if platform != nil {
		candidates = nil
		matcher := platforms.OnlyStrict(platforms.Normalize(*platform))
		for _, pin := range pins {
			if pin.platform == nil || matcher.Match(platforms.Normalize(*pin.platform)) {
				candidates = append(candidates, pin)
			}
		}
		if len(candidates) == 0 {
			return "", true, fmt.Sprintf("replay pin: no recorded pin for %s on platform %s", source, platforms.Format(*platform))
		}
	}
	if pinned, ok := uniquePinDigest(candidates); ok {
		return pinned, true, ""
	}
	if observed != "" {
		for _, pin := range candidates {
			if pin.digest == observed {
				return observed, true, ""
			}
		}
	}
	return "", true, fmt.Sprintf("replay pin: ambiguous provenance pins for %s", source)
}

func convertPinnedSource(req *policysession.CheckPolicyRequest, pinned digest.Digest) (*policysession.DecisionResponse, bool, error) {
	if req == nil || req.Source == nil || req.Source.GetSource() == nil {
		return nil, false, nil
	}
	src := req.Source.GetSource()
	if canon := canonicalRequestSource(req); canon != "" && canon != src.Identifier {
		clone := src.CloneVT()
		clone.Identifier = canon
		src = clone
	}
	switch {
	case strings.HasPrefix(src.Identifier, "docker-image://"):
		newSrc, err := policy.AddPinToImage(src, pinned)
		if err != nil {
			return nil, false, errors.Wrap(err, "failed to pin covered image source")
		}
		if newSrc.Identifier == src.Identifier {
			return nil, true, nil
		}
		return convertResponse(newSrc), true, nil
	case strings.HasPrefix(src.Identifier, "http://"), strings.HasPrefix(src.Identifier, "https://"):
		return convertPinnedAttr(src, solverpb.AttrHTTPChecksum, pinned.String()), true, nil
	case strings.HasPrefix(src.Identifier, "git://"):
		return convertPinnedAttr(src, solverpb.AttrGitChecksum, pinned.Encoded()), true, nil
	default:
		return nil, false, nil
	}
}

func convertPinnedAttr(src *solverpb.SourceOp, key, value string) *policysession.DecisionResponse {
	if src.Attrs[key] == value {
		return nil
	}
	newSrc := src.CloneVT()
	if newSrc.Attrs == nil {
		newSrc.Attrs = map[string]string{}
	}
	newSrc.Attrs[key] = value
	return convertResponse(newSrc)
}

func convertResponse(src *solverpb.SourceOp) *policysession.DecisionResponse {
	return &policysession.DecisionResponse{
		Action: spb.PolicyAction_CONVERT,
		Update: src,
	}
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
		if v := g.GetCommitChecksum(); v != "" && dgst == "" {
			dgst = gitDigest(v)
		}
	}
	if uri != "" {
		uri, dgst = normalizeRequestSourceIdentity(uri, dgst)
	}
	return uri, dgst
}

func gitDigest(v string) digest.Digest {
	if _, err := digest.Parse(v); err == nil {
		return digest.Digest(v)
	}
	switch len(v) {
	case 40:
		return digest.NewDigestFromEncoded(digest.Algorithm("sha1"), v)
	case 64:
		return digest.NewDigestFromEncoded(digest.SHA256, v)
	default:
		return digest.Digest(v)
	}
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

func canonicalMaterialIdentifier(m slsa1.ResourceDescriptor) (string, *ocispecs.Platform, bool) {
	src, platform, err := policy.ParseSLSAMaterial(m)
	if err != nil || src == nil || src.Identifier == "" {
		return "", nil, false
	}
	return src.Identifier, platform, true
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
	if _, refDigest, ok := strings.Cut(refStr, "@"); ok {
		if dgst == "" {
			if parsed, err := digest.Parse(refDigest); err == nil {
				dgst = parsed
			}
		}
		return uri, dgst
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
