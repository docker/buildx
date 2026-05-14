package replay

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/containerd/containerd/v2/core/content"
	contentlocal "github.com/containerd/containerd/v2/plugins/content/local"
	"github.com/containerd/platforms"
	"github.com/docker/buildx/util/imagetools"
	"github.com/docker/buildx/util/ocilayout"
	"github.com/moby/buildkit/client/ociindex"
	"github.com/moby/buildkit/util/contentutil"
	digest "github.com/opencontainers/go-digest"
	ocispecs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/pkg/errors"
)

// MaterialsResolver resolves provenance materials to a (descriptor, provider)
// pair the replay pipeline can use to serve content locally. Lookup order:
//
//  1. overrides keyed by URI or "sha256:<digest>"
//  2. explicit stores, in the order listed on `--materials`
//  3. the `provenance` sentinel (fetch from the URI recorded in provenance)
//
// Snapshot-backed store lookup (registry-native snapshot OCI indexes) is
// implemented in a later slice — this slice supports overrides, generic
// OCI-layout stores (image digests), filesystem content stores (sha256
// addressed), and the provenance sentinel fallback.
type MaterialsResolver struct {
	overrides map[string]materialOverride
	stores    []materialStore
	sentinel  bool // true when `provenance` sentinel is enabled
}

type materialOverride struct {
	// spec is the raw right-hand side of a `key=value` override. Resolved
	// lazily so a bad override doesn't poison the whole resolver build.
	spec string
}

// materialStore is an entry on the --materials list that is NOT an override.
// Exactly one of path/ociLayout is populated.
type materialStore struct {
	// path is a filesystem directory laid out as a raw content store
	// (blobs/<alg>/<hex>).
	path string
	// ociLayout is an absolute path to an OCI image layout directory.
	// Lookup is by descriptor digest only.
	ociLayout string
}

// NewMaterialsResolver parses the --materials list and returns a resolver.
// Spec forms accepted:
//
//   - "provenance"                         — sentinel (default when the
//     --materials list is empty)
//   - "oci-layout://<path>[:<tag>]"        — OCI layout store
//   - "<absolute-path>"                    — raw content store
//     (blobs/<alg>/<hex>)
//   - "<key>=<spec>"                       — override: <key> is the URI or
//     "sha256:<digest>"; <spec> is any of
//     the above narrowed to one blob.
//
// `registry://<ref>` is parsed and reserved but errs as not-yet-implemented
// in this slice.
func NewMaterialsResolver(specs []string) (*MaterialsResolver, error) {
	r := &MaterialsResolver{
		overrides: map[string]materialOverride{},
	}

	// Default behavior when the user supplied no stores: use the provenance
	// sentinel (fetch from the URIs recorded on each material).
	if len(specs) == 0 {
		r.sentinel = true
		return r, nil
	}

	for _, raw := range specs {
		spec := strings.TrimSpace(raw)
		if spec == "" {
			continue
		}

		// Override: split on first '=' whose LHS is not a URI scheme.
		// "<uri>=<store>" — the LHS starts with a scheme and carries "://",
		// so the '=' we want is after the "://...". Detect by looking for
		// the first '=' that is NOT inside a URI's path portion. We keep
		// this simple: if the token before the first '=' is one of
		// "provenance", an oci-layout:// prefix, or an absolute path, it
		// is not an override.
		if isOverrideSpec(spec) {
			key, val, ok := splitOverride(spec)
			if !ok {
				return nil, errors.Errorf("malformed --materials override %q (want <key>=<value>)", spec)
			}
			r.overrides[key] = materialOverride{spec: val}
			continue
		}

		switch {
		case spec == "provenance":
			r.sentinel = true
		case strings.HasPrefix(spec, "oci-layout://"):
			ref, _, err := ocilayout.Parse(spec)
			if err != nil {
				return nil, errors.Wrapf(err, "invalid --materials oci-layout spec %q", spec)
			}
			r.stores = append(r.stores, materialStore{ociLayout: ref.Path})
		case strings.HasPrefix(spec, "registry://"):
			// Reserved for snapshot-backed store lookup (slice B).
			return nil, ErrNotImplemented("registry:// materials store")
		case filepath.IsAbs(spec):
			if fi, err := os.Stat(spec); err != nil || !fi.IsDir() {
				return nil, errors.Errorf("--materials path %q is not a directory", spec)
			}
			r.stores = append(r.stores, materialStore{path: spec})
		default:
			return nil, errors.Errorf("unrecognized --materials spec %q", spec)
		}
	}

	return r, nil
}

// isOverrideSpec decides whether a raw --materials token is shaped like
// "<key>=<value>". A token that starts with a known store sentinel/prefix or
// is an absolute path is never an override, even if it happens to contain an
// '=' somewhere inside (e.g. "oci-layout:///path/to/layout:tag=foo").
func isOverrideSpec(spec string) bool {
	switch {
	case spec == "provenance":
		return false
	case strings.HasPrefix(spec, "oci-layout://"):
		return false
	case strings.HasPrefix(spec, "registry://"):
		return false
	case filepath.IsAbs(spec):
		return false
	}
	return strings.Contains(spec, "=")
}

func splitOverride(spec string) (key, val string, ok bool) {
	i := strings.IndexByte(spec, '=')
	if i <= 0 || i >= len(spec)-1 {
		return "", "", false
	}
	return spec[:i], spec[i+1:], true
}

// Sentinel reports whether the `provenance` sentinel is enabled. The sentinel
// authorises a fallback fetch from the URI recorded in the provenance.
func (r *MaterialsResolver) Sentinel() bool {
	return r != nil && r.sentinel
}

// Overrides returns an iteration-stable copy of the configured overrides for
// tests and dry-run inspection.
func (r *MaterialsResolver) Overrides() map[string]string {
	if r == nil {
		return nil
	}
	out := make(map[string]string, len(r.overrides))
	for k, v := range r.overrides {
		out[k] = v.spec
	}
	return out
}

// HasStores reports whether any explicit stores are configured. The primary
// use is driving behavior when the resolver has only a sentinel.
func (r *MaterialsResolver) HasStores() bool {
	return r != nil && len(r.stores) > 0
}

// ResolveOption customises a Resolve call.
type ResolveOption func(*resolveOptions)

type resolveOptions struct {
	// platform is used when resolving image materials out of a
	// snapshot-backed store: the materials-manifest carries the original
	// root-index blob, and the snapshot index's manifests[] holds the
	// per-platform children. The caller supplies the platform it is
	// replaying.
	platform *ocispecs.Platform
	// builderPlatform is the platform the original builder ran on
	// (predicate InternalParameters.builderPlatform). Used as the
	// fall-back when no child matches the subject platform — frontend
	// images and cross-compile toolchains run on the builder host, not
	// the target.
	builderPlatform *ocispecs.Platform
}

// WithPlatform attaches a target platform to a Resolve call. When resolving
// an image material against a snapshot-backed store this selects the
// per-platform child to return.
func WithPlatform(p *ocispecs.Platform) ResolveOption {
	return func(o *resolveOptions) { o.platform = p }
}

// WithBuilderPlatform attaches the original builder's platform (recorded in
// provenance) so the snapshot-backed store can fall back to it when the
// subject platform has no match in an image material's root index.
func WithBuilderPlatform(p ocispecs.Platform) ResolveOption {
	return func(o *resolveOptions) { o.builderPlatform = &p }
}

// Resolve returns the descriptor and content.Provider that serve the material
// with the given (uri, dgst). Exactly one of uri / dgst may be empty; when
// both are empty an error is returned.
//
// Strict by default: materials not covered by the configured stores / overrides
// and not reachable via the sentinel produce MaterialNotFoundError. The
// provenance sentinel is NOT a network fetch in this slice — it signals that
// BuildKit may resolve the material itself, subject to the source-policy
// pin callback. A caller that requires a concrete (descriptor, provider)
// pair for a sentinel-only material should use the store-backed
// resolution path.
//
// Snapshot-backed stores are detected at lookup time. When `dgst` matches a
// snapshot's materials-manifest layer AND the layer's media type is an OCI
// image index (an image-material root index stashed by `replay snapshot`),
// Resolve returns the platform-specific child manifest descriptor reachable
// through the snapshot index's `manifests[]`. The caller should pass
// WithPlatform so the correct child can be picked.
func (r *MaterialsResolver) Resolve(ctx context.Context, uri string, dgst digest.Digest, opts ...ResolveOption) (ocispecs.Descriptor, content.Provider, error) {
	if r == nil {
		return ocispecs.Descriptor{}, nil, ErrMaterialNotFound(uri, dgst.String())
	}
	if uri == "" && dgst == "" {
		return ocispecs.Descriptor{}, nil, errors.New("resolve called with empty uri and empty digest")
	}

	var ro resolveOptions
	for _, opt := range opts {
		opt(&ro)
	}

	// 1. Overrides.
	if key, o, ok := r.lookupOverride(uri, dgst); ok {
		desc, provider, err := r.resolveOverride(ctx, o, dgst, &ro)
		if err != nil {
			return ocispecs.Descriptor{}, nil, errors.Wrapf(err, "override %s", key)
		}
		return desc, provider, nil
	}

	// 2. Explicit stores, in order. Snapshot-backed lookup is preferred
	//    when a store root carries a snapshot index at its root.
	for _, s := range r.stores {
		if dgst == "" {
			continue
		}
		if desc, provider, ok, err := s.lookupSnapshot(ctx, dgst, &ro); err != nil {
			return ocispecs.Descriptor{}, nil, err
		} else if ok {
			return desc, provider, nil
		}
		desc, provider, ok, err := s.lookupByDigest(ctx, dgst)
		if err != nil {
			return ocispecs.Descriptor{}, nil, err
		}
		if ok {
			return desc, provider, nil
		}
	}

	// 3. Sentinel fallback. Replay relies on BuildKit fetching the material
	//    over the network subject to the policy callback; we cannot
	//    materialise the content locally without going online, so we
	//    surface a sentinel-only descriptor (empty provider) that the
	//    caller may use to signal "let BuildKit resolve".
	if r.sentinel {
		return ocispecs.Descriptor{Digest: dgst}, nil, nil
	}

	return ocispecs.Descriptor{}, nil, ErrMaterialNotFound(uri, dgst.String())
}

func (r *MaterialsResolver) lookupOverride(uri string, dgst digest.Digest) (string, materialOverride, bool) {
	if uri != "" {
		if o, ok := r.overrides[uri]; ok {
			return uri, o, true
		}
	}
	if dgst != "" {
		if o, ok := r.overrides[dgst.String()]; ok {
			return dgst.String(), o, true
		}
	}
	return "", materialOverride{}, false
}

// resolveOverride resolves an override value to a concrete (descriptor,
// provider) pair. Override values accept the same forms as non-override
// specs: "oci-layout://<path>[:<tag>]", "<absolute-path>".
func (r *MaterialsResolver) resolveOverride(ctx context.Context, o materialOverride, dgst digest.Digest, ro *resolveOptions) (ocispecs.Descriptor, content.Provider, error) {
	spec := strings.TrimSpace(o.spec)
	switch {
	case strings.HasPrefix(spec, "oci-layout://"):
		ref, _, err := ocilayout.Parse(spec)
		if err != nil {
			return ocispecs.Descriptor{}, nil, err
		}
		store := materialStore{ociLayout: ref.Path}
		if dgst == "" {
			return ocispecs.Descriptor{}, nil, errors.New("override oci-layout requires a material digest")
		}
		if desc, provider, ok, err := store.lookupSnapshot(ctx, dgst, ro); err != nil {
			return ocispecs.Descriptor{}, nil, err
		} else if ok {
			return desc, provider, nil
		}
		desc, provider, ok, err := store.lookupByDigest(ctx, dgst)
		if err != nil {
			return ocispecs.Descriptor{}, nil, err
		}
		if !ok {
			return ocispecs.Descriptor{}, nil, ErrMaterialNotFound("", dgst.String())
		}
		return desc, provider, nil
	case filepath.IsAbs(spec):
		fi, err := os.Stat(spec)
		if err != nil {
			return ocispecs.Descriptor{}, nil, errors.WithStack(err)
		}
		if fi.IsDir() {
			store := materialStore{path: spec}
			if dgst == "" {
				return ocispecs.Descriptor{}, nil, errors.New("override path requires a material digest")
			}
			if desc, provider, ok, err := store.lookupSnapshot(ctx, dgst, ro); err != nil {
				return ocispecs.Descriptor{}, nil, err
			} else if ok {
				return desc, provider, nil
			}
			desc, provider, ok, err := store.lookupByDigest(ctx, dgst)
			if err != nil {
				return ocispecs.Descriptor{}, nil, err
			}
			if !ok {
				return ocispecs.Descriptor{}, nil, ErrMaterialNotFound("", dgst.String())
			}
			return desc, provider, nil
		}
		// A file override addresses exactly one blob. We expose it as a
		// synthetic provider rooted at the file's bytes.
		dt, err := os.ReadFile(spec)
		if err != nil {
			return ocispecs.Descriptor{}, nil, errors.WithStack(err)
		}
		actual := digest.FromBytes(dt)
		if dgst != "" && actual != dgst {
			return ocispecs.Descriptor{}, nil, errors.Errorf("override file %s has digest %s, want %s", spec, actual, dgst)
		}
		desc := ocispecs.Descriptor{Digest: actual, Size: int64(len(dt))}
		buf := contentutil.NewBuffer()
		if err := content.WriteBlob(ctx, buf, actual.String(), bytes.NewReader(dt), desc); err != nil {
			return ocispecs.Descriptor{}, nil, errors.WithStack(err)
		}
		return desc, buf, nil
	}
	return ocispecs.Descriptor{}, nil, errors.Errorf("unsupported override value %q", spec)
}

// lookupByDigest serves a blob by digest from a filesystem or oci-layout
// store. The returned descriptor has Digest+Size populated; MediaType is
// left empty — callers that need it must inspect the bytes.
func (s materialStore) lookupByDigest(ctx context.Context, dgst digest.Digest) (ocispecs.Descriptor, content.Provider, bool, error) {
	if dgst == "" {
		return ocispecs.Descriptor{}, nil, false, nil
	}
	root := s.path
	if root == "" {
		root = s.ociLayout
	}
	if root == "" {
		return ocispecs.Descriptor{}, nil, false, nil
	}
	blobPath := filepath.Join(root, "blobs", dgst.Algorithm().String(), dgst.Encoded())
	fi, err := os.Stat(blobPath)
	if err != nil {
		if os.IsNotExist(err) {
			return ocispecs.Descriptor{}, nil, false, nil
		}
		return ocispecs.Descriptor{}, nil, false, errors.WithStack(err)
	}
	if fi.IsDir() {
		return ocispecs.Descriptor{}, nil, false, errors.Errorf("blob path %s is a directory", blobPath)
	}
	desc := ocispecs.Descriptor{Digest: dgst, Size: fi.Size()}

	// An OCI-layout store is expected to be a real containerd content
	// store; use contentlocal.NewStore so Readers are proper.
	provider, err := contentlocal.NewStore(root)
	if err != nil {
		return ocispecs.Descriptor{}, nil, false, errors.Wrapf(err, "store at %s", root)
	}
	// Ensure the blob is actually readable (guards against partial layouts).
	ra, err := provider.ReaderAt(ctx, desc)
	if err != nil {
		return ocispecs.Descriptor{}, nil, false, errors.WithStack(err)
	}
	_ = ra.Close()
	return desc, provider, true, nil
}

// ParseLocationForMaterials exposes imagetools.ParseLocation for the
// resolver's callers. Kept here as a thin alias so consumers don't need the
// imagetools import just to parse an override right-hand side.
func ParseLocationForMaterials(s string) (*imagetools.Location, error) {
	return imagetools.ParseLocation(s)
}

// lookupSnapshot attempts to serve `dgst` through a snapshot-backed view of
// the store. The store's root is inspected once per call;
// if it carries a snapshot index (artifactType = ArtifactTypeSnapshot) the
// lookup proceeds by:
//
//  1. Scanning every per-platform snapshot index's materials-manifest
//     layers for a layer whose digest matches `dgst`. When found, the
//     layer descriptor and a filesystem-backed provider are returned. If
//     the matched layer's media type is an OCI image index (i.e. the
//     original image-material root index kept opaque in the snapshot),
//     the function parses the root index, selects the platform child
//     matching ro.platform, and returns that child's descriptor as
//     reachable from the per-platform snapshot index's manifests[].
//  2. Scanning the per-platform snapshot index's manifests[] directly so
//     a caller that already has the platform manifest's digest can
//     resolve it without going through the root.
//
// Returns ok == false when the store is not snapshot-shaped or when the
// digest is not covered. In both cases the caller falls back to a plain
// digest lookup.
func (s materialStore) lookupSnapshot(ctx context.Context, dgst digest.Digest, ro *resolveOptions) (ocispecs.Descriptor, content.Provider, bool, error) {
	if s.ociLayout == "" {
		return ocispecs.Descriptor{}, nil, false, nil
	}
	root, roots, err := readSnapshotRoots(s.ociLayout)
	if err != nil {
		// A store that just isn't a snapshot: fall back to plain lookup.
		return ocispecs.Descriptor{}, nil, false, nil
	}
	if root.ArtifactType != ArtifactTypeSnapshot && !anyIsSnapshot(roots) {
		return ocispecs.Descriptor{}, nil, false, nil
	}

	store, err := contentlocal.NewStore(s.ociLayout)
	if err != nil {
		return ocispecs.Descriptor{}, nil, false, errors.Wrapf(err, "store at %s", s.ociLayout)
	}

	perPlatformDescs, err := collectPerPlatformSnapshotIndexes(ctx, store, root, roots)
	if err != nil {
		return ocispecs.Descriptor{}, nil, false, err
	}

	var (
		wantPlat    *ocispecs.Platform
		builderPlat = platforms.DefaultSpec()
	)
	if ro != nil {
		wantPlat = ro.platform
		if ro.builderPlatform != nil {
			builderPlat = *ro.builderPlatform
		}
	}

	for _, ppDesc := range perPlatformDescs {
		ppDt, err := content.ReadBlob(ctx, store, ppDesc)
		if err != nil {
			return ocispecs.Descriptor{}, nil, false, errors.WithStack(err)
		}
		var pp ocispecs.Index
		if err := json.Unmarshal(ppDt, &pp); err != nil {
			return ocispecs.Descriptor{}, nil, false, errors.WithStack(err)
		}

		// Load the materials manifest (first manifest with artifactType
		// ArtifactTypeMaterials — may be absent when the snapshot was
		// created with --include-materials=false).
		var materialsLayers []ocispecs.Descriptor
		for _, m := range pp.Manifests {
			if m.ArtifactType != ArtifactTypeMaterials {
				continue
			}
			mData, err := content.ReadBlob(ctx, store, m)
			if err != nil {
				return ocispecs.Descriptor{}, nil, false, errors.WithStack(err)
			}
			var mm ocispecs.Manifest
			if err := json.Unmarshal(mData, &mm); err != nil {
				return ocispecs.Descriptor{}, nil, false, errors.WithStack(err)
			}
			materialsLayers = mm.Layers
			break
		}

		// 1a. Direct hit on a materials-manifest layer.
		for _, l := range materialsLayers {
			if l.Digest != dgst {
				continue
			}
			if isIndexMediaType(l.MediaType) {
				// Image material root — pick the platform-specific child
				// from the per-platform index's manifests[].
				child, err := pickPerPlatformChild(ctx, store, l, pp, wantPlat, builderPlat)
				if err != nil {
					return ocispecs.Descriptor{}, nil, false, err
				}
				return child, store, true, nil
			}
			return l, store, true, nil
		}

		// 1b. Direct hit on a manifests[] entry (a platform-specific image
		//     manifest descriptor that the caller already looked up).
		for _, m := range pp.Manifests {
			if m.Digest == dgst {
				return m, store, true, nil
			}
		}
	}

	return ocispecs.Descriptor{}, nil, false, nil
}

// readSnapshotRoots loads the root manifest references from the store's
// index.json. Returns the single-descriptor "root" when only one is present
// (the per-platform case) plus the full list for multi-platform snapshots.
func readSnapshotRoots(path string) (ocispecs.Descriptor, []ocispecs.Descriptor, error) {
	idx, err := ociindex.NewStoreIndex(path).Read()
	if err != nil {
		return ocispecs.Descriptor{}, nil, err
	}
	if len(idx.Manifests) == 0 {
		return ocispecs.Descriptor{}, nil, errors.New("empty index")
	}
	return idx.Manifests[0], idx.Manifests, nil
}

// anyIsSnapshot reports whether any descriptor in roots carries the snapshot
// artifact type. A top-level multi-platform snapshot's descriptor may itself
// carry artifactType = ArtifactTypeSnapshot; so will each per-platform child.
func anyIsSnapshot(roots []ocispecs.Descriptor) bool {
	for _, r := range roots {
		if r.ArtifactType == ArtifactTypeSnapshot {
			return true
		}
	}
	return false
}

// collectPerPlatformSnapshotIndexes traverses the supplied roots and returns
// every per-platform snapshot index descriptor reachable from them. A
// per-platform snapshot index is identified by mediaType=image index and
// artifactType = ArtifactTypeSnapshot with a `subject` (§5.2.1). The
// single-platform case returns the root itself; the multi-platform case
// unwraps the top-level index and returns its children.
func collectPerPlatformSnapshotIndexes(ctx context.Context, store content.Provider, root ocispecs.Descriptor, roots []ocispecs.Descriptor) ([]ocispecs.Descriptor, error) {
	candidates := roots
	if len(candidates) == 0 {
		candidates = []ocispecs.Descriptor{root}
	}
	var out []ocispecs.Descriptor
	for _, c := range candidates {
		if !isIndexMediaType(c.MediaType) {
			continue
		}
		dt, err := content.ReadBlob(ctx, store, c)
		if err != nil {
			return nil, errors.WithStack(err)
		}
		var idx ocispecs.Index
		if err := json.Unmarshal(dt, &idx); err != nil {
			return nil, errors.WithStack(err)
		}
		// A per-platform snapshot index carries a non-nil Subject (§5.2.1).
		if idx.Subject != nil {
			out = append(out, c)
			continue
		}
		// Top-level multi-platform index — unwrap one level.
		for _, child := range idx.Manifests {
			if child.ArtifactType == ArtifactTypeSnapshot && isIndexMediaType(child.MediaType) {
				out = append(out, child)
			}
		}
	}
	return out, nil
}

// pickPerPlatformChild selects the platform-specific manifest from a
// per-platform snapshot index's manifests[] that corresponds to the
// recorded image-material root `rootLayer`. The matcher prefers the
// subject's platform then falls back to the builder platform; when the
// root index has a single child it is returned unconditionally.
func pickPerPlatformChild(ctx context.Context, store content.Provider, rootLayer ocispecs.Descriptor, pp ocispecs.Index, wantPlat *ocispecs.Platform, builderPlat ocispecs.Platform) (ocispecs.Descriptor, error) {
	dt, err := content.ReadBlob(ctx, store, rootLayer)
	if err != nil {
		return ocispecs.Descriptor{}, errors.WithStack(err)
	}
	var rootIdx ocispecs.Index
	if err := json.Unmarshal(dt, &rootIdx); err != nil {
		return ocispecs.Descriptor{}, errors.WithStack(err)
	}
	matcher := replayPlatformMatcher(wantPlat, builderPlat)
	var best *ocispecs.Descriptor
	for i := range rootIdx.Manifests {
		c := rootIdx.Manifests[i]
		if c.Platform == nil || !matcher.Match(*c.Platform) {
			continue
		}
		if best == nil || matcher.Less(*c.Platform, *best.Platform) {
			best = &c
		}
	}
	var wantDgst digest.Digest
	switch {
	case best != nil:
		wantDgst = best.Digest
	case len(rootIdx.Manifests) == 1:
		wantDgst = rootIdx.Manifests[0].Digest
	default:
		return ocispecs.Descriptor{}, errors.Errorf("snapshot lookup: root %s has no child matching subject platform %s or builder %s", rootLayer.Digest, formatPlatformPtr(wantPlat), platforms.Format(builderPlat))
	}
	// Resolve against manifests[] for the concrete descriptor (includes
	// size / mediaType as recorded by the snapshot).
	for _, m := range pp.Manifests {
		if m.Digest == wantDgst {
			return m, nil
		}
	}
	// Not present in the snapshot's manifests[] — return a synthetic
	// descriptor so the caller can still address content-by-digest.
	return ocispecs.Descriptor{
		MediaType: ocispecs.MediaTypeImageManifest,
		Digest:    wantDgst,
	}, nil
}

func isIndexMediaType(mt string) bool {
	return mt == ocispecs.MediaTypeImageIndex || mt == "application/vnd.docker.distribution.manifest.list.v2+json"
}
