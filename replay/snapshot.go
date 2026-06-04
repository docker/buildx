package replay

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/containerd/containerd/v2/core/content"
	"github.com/containerd/containerd/v2/core/images"
	"github.com/containerd/containerd/v2/core/remotes"
	contentlocal "github.com/containerd/containerd/v2/plugins/content/local"
	"github.com/containerd/errdefs"
	"github.com/containerd/platforms"
	"github.com/docker/buildx/builder"
	"github.com/docker/buildx/util/buildflags"
	"github.com/docker/buildx/util/imagetools"
	"github.com/docker/buildx/util/progress"
	"github.com/docker/cli/cli/command"
	slsa1 "github.com/in-toto/in-toto-golang/in_toto/slsa_provenance/v1"
	"github.com/moby/buildkit/client"
	"github.com/moby/buildkit/client/ociindex"
	"github.com/moby/buildkit/util/contentutil"
	"github.com/moby/buildkit/util/purl"
	digest "github.com/opencontainers/go-digest"
	ocispecs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/pkg/errors"
)

// withMediaTypeKeyPrefix registers ref-key prefixes for non-standard media
// types that the snapshot chain walker will encounter. Without this,
// containerd's remotes.MakeRefKey falls to its default branch and logs
// "reference for unknown type: …" when copying the OCI 1.1 empty-config
// blob that sits inside buildx attestation manifests.
func withMediaTypeKeyPrefix(ctx context.Context) context.Context {
	return remotes.WithMediaTypeKeyPrefix(ctx, "application/vnd.oci.empty.v1+json", "empty")
}

// SnapshotRequest is the input to Snapshot.
type SnapshotRequest struct {
	// Targets are the per-platform (subject, predicate) pairs to snapshot.
	// Each subject must carry a non-empty AttestationManifest descriptor
	// (image / oci-layout subjects only; attestation-file inputs are
	// rejected upstream).
	Targets []Target
	// IncludeMaterials controls whether material content is copied and the
	// materials artifact manifest is emitted.
	IncludeMaterials bool
	// Materials resolves image / http / container-blob materials to a local
	// (descriptor, provider) pair. Required when IncludeMaterials is true.
	Materials *MaterialsResolver
	// Output is the parsed --output spec. Exactly one form is allowed
	// (local / oci / registry).
	Output *buildflags.ExportEntry
	// Progress receives step events and non-fatal warnings. May be nil —
	// in that case events are silently dropped.
	Progress progress.Writer
}

// Snapshot produces a replay snapshot for the supplied subjects/predicates and
// writes it through the configured --output target. This function does NOT
// invoke build.Build — snapshot is pure content movement plus manifest
// assembly.
//
// dockerCli + builderName are consumed only to construct a buildx image
// resolver so that image materials can be fetched from their recorded
// registries. They may be zero when all materials are resolvable purely via
// req.Materials (e.g. tests that pre-pin an --materials=oci-layout store).
func Snapshot(ctx context.Context, dockerCli command.Cli, builderName string, req *SnapshotRequest) error {
	if req == nil {
		return errors.New("nil snapshot request")
	}
	if req.Output == nil {
		return errors.New("snapshot: --output is required")
	}
	stage, root, _, err := assembleSnapshot(ctx, dockerCli, builderName, req)
	if err != nil {
		return err
	}
	return writeSnapshotOutput(ctx, stage, root, req.Output, dockerCli, builderName)
}

// assembleSnapshot runs the staging phase shared by real-run and dry-run:
// validates targets, stages every blob the snapshot would emit, and
// returns the root descriptor plus per-target staging stores (for dry-run
// consumers that want per-target artifact lists). The real run only cares
// about the first store and root.
func assembleSnapshot(ctx context.Context, dockerCli command.Cli, builderName string, req *SnapshotRequest) (*stagingStore, ocispecs.Descriptor, []*stagingStore, error) {
	if req == nil {
		return nil, ocispecs.Descriptor{}, nil, errors.New("nil snapshot request")
	}
	if len(req.Targets) == 0 {
		return nil, ocispecs.Descriptor{}, nil, errors.New("no targets to snapshot")
	}

	// Register ref-key prefixes for non-standard media types so
	// containerd does not log spurious "reference for unknown type"
	// warnings while copying the attestation chain.
	ctx = withMediaTypeKeyPrefix(ctx)

	// progress logger — nop when the caller did not supply a Writer.
	var pwlog progress.Logger = func(*client.SolveStatus) {}
	if req.Progress != nil {
		pwlog = req.Progress.Write
	}

	// Shared warn-once ledger: each (category, key) pair prints at most
	// one warning across the whole snapshot (e.g. the same git URI
	// referenced from every platform should not flood output).
	warn := newWarnOnce()

	// One staging store per target — gives dry-run a clean per-target
	// descriptor list and keeps the real-run assembly deterministic.
	stages := make([]*stagingStore, 0, len(req.Targets))
	// Merged stage for real-run output. Each target's stage is folded in
	// after assembly so a single flush writes everything.
	merged := newStagingStore()

	// Lazily-constructed registry resolver for image materials (reused across
	// subjects). buildx resolver is nil-safe so we keep the builder/auth setup
	// deferred until an image material is seen.
	var registryResolver *imagetools.Resolver
	lazyResolver := func() (*imagetools.Resolver, error) {
		if registryResolver != nil {
			return registryResolver, nil
		}
		if dockerCli == nil {
			registryResolver = imagetools.New(imagetools.Opt{})
			return registryResolver, nil
		}
		b, err := builder.New(dockerCli, builder.WithName(builderName))
		if err != nil {
			return nil, err
		}
		imgOpt, err := b.ImageOpt()
		if err != nil {
			return nil, err
		}
		registryResolver = imagetools.New(imgOpt)
		return registryResolver, nil
	}

	emptyConfigDesc := ocispecs.Descriptor{
		MediaType: ociEmptyConfigMediaType,
		Digest:    digest.Digest(ociEmptyConfigDigest),
		Size:      ociEmptyConfigSize,
	}

	perPlatformDescs := make([]ocispecs.Descriptor, 0, len(req.Targets))

	for ti, t := range req.Targets {
		s, pred := t.Subject, t.Predicate
		if s == nil || pred == nil {
			return nil, ocispecs.Descriptor{}, nil, errors.New("target has nil subject or predicate")
		}
		if s.IsAttestationFile() {
			return nil, ocispecs.Descriptor{}, nil, ErrUnsupportedSubject("snapshot requires an image or oci-layout subject")
		}
		if s.AttestationManifest().Digest == "" {
			return nil, ocispecs.Descriptor{}, nil, ErrNoProvenance(s.InputRef())
		}

		stage := newStagingStore()
		if err := stage.writeRaw(ctx, emptyConfigDesc, OCIEmptyConfigBytes()); err != nil {
			return nil, ocispecs.Descriptor{}, nil, errors.Wrap(err, "write empty config")
		}

		var ppDesc ocispecs.Descriptor
		targetName := fmt.Sprintf("[%d/%d] snapshot %s", ti+1, len(req.Targets), snapshotTargetLabel(s))
		err := progress.Wrap(targetName, pwlog, func(sub progress.SubLogger) error {
			d, err := snapshotOneTarget(ctx, stage, s, pred, req, lazyResolver, warn, sub)
			if err != nil {
				return err
			}
			ppDesc = d
			return nil
		})
		if err != nil {
			return nil, ocispecs.Descriptor{}, nil, err
		}
		if s.Descriptor.Platform != nil {
			p := *s.Descriptor.Platform
			ppDesc.Platform = &p
		}
		perPlatformDescs = append(perPlatformDescs, ppDesc)
		stages = append(stages, stage)

		if err := mergeStage(ctx, merged, stage); err != nil {
			return nil, ocispecs.Descriptor{}, nil, err
		}
	}

	// Root descriptor that the output writer addresses.
	var root ocispecs.Descriptor
	if len(perPlatformDescs) == 1 {
		root = perPlatformDescs[0]
	} else {
		_, rootDesc, rootData, err := MultiPlatformSnapshotIndex(perPlatformDescs)
		if err != nil {
			return nil, ocispecs.Descriptor{}, nil, err
		}
		if err := merged.writeRaw(ctx, ocispecs.Descriptor{
			MediaType: ocispecs.MediaTypeImageIndex,
			Digest:    rootDesc.Digest,
			Size:      rootDesc.Size,
		}, rootData); err != nil {
			return nil, ocispecs.Descriptor{}, nil, errors.Wrap(err, "write multi-platform snapshot index")
		}
		root = rootDesc
	}

	return merged, root, stages, nil
}

// mergeStage copies every blob from src into dst. Used to fold per-target
// stages into the merged stage that the real-run output writer flushes.
func mergeStage(ctx context.Context, dst, src *stagingStore) error {
	src.mu.Lock()
	order := slices.Clone(src.order)
	src.mu.Unlock()
	for _, dgst := range order {
		desc := src.descs[dgst]
		ra, err := src.ReaderAt(ctx, desc)
		if err != nil {
			return errors.Wrapf(err, "read %s", dgst)
		}
		err = content.WriteBlob(ctx, dst, "snapshot-"+dgst.String(), content.NewReader(ra), desc)
		ra.Close()
		if err != nil && !errdefs.IsAlreadyExists(err) {
			return errors.Wrapf(err, "merge %s", dgst)
		}
		dst.record(desc)
	}
	return nil
}

// snapshotOneTarget copies the attestation chain and each material for a
// single (subject, predicate) target into the staging buffer and assembles
// the per-platform snapshot index. Each unit of work is wrapped in a
// progress sub-step so the caller's printer can render what is happening.
func snapshotOneTarget(
	ctx context.Context,
	stage *stagingStore,
	s *Subject,
	pred *Predicate,
	req *SnapshotRequest,
	lazyResolver func() (*imagetools.Resolver, error),
	warn *warnOnce,
	sub progress.SubLogger,
) (ocispecs.Descriptor, error) {
	attestMfst := s.AttestationManifest()
	if err := sub.Wrap(fmt.Sprintf("copy attestation manifest %s", attestMfst.Digest), func() error {
		return contentutil.CopyChain(ctx, stage, s.Provider, attestMfst)
	}); err != nil {
		return ocispecs.Descriptor{}, errors.Wrapf(err, "copy attestation manifest chain for %s", s.InputRef())
	}

	var (
		materialsLayers []ocispecs.Descriptor
		imageMfsts      []ocispecs.Descriptor
	)
	seenLayerDigest := map[digest.Digest]struct{}{}
	seenImageMfst := map[digest.Digest]struct{}{}

	for _, m := range pred.ResolvedDependencies() {
		switch classifyMaterial(m) {
		case materialKindImage:
			if !req.IncludeMaterials {
				continue
			}
			rootDgst := resourceDigest(m)
			if rootDgst == "" {
				return ocispecs.Descriptor{}, errors.Errorf("image material %q has no sha256 digest", m.URI)
			}
			if err := sub.Wrap(fmt.Sprintf("image material %s", m.URI), func() error {
				rootDesc, rootProvider, err := resolveImageMaterial(ctx, req.Materials, lazyResolver, m, rootDgst, WithPlatform(s.Descriptor.Platform), WithBuilderPlatform(pred.BuilderPlatform()))
				if err != nil {
					return err
				}
				if err := stage.copyBlob(ctx, rootProvider, rootDesc); err != nil {
					return errors.Wrapf(err, "copy image root %s", rootDesc.Digest)
				}
				if _, ok := seenLayerDigest[rootDesc.Digest]; !ok {
					seenLayerDigest[rootDesc.Digest] = struct{}{}
					materialsLayers = append(materialsLayers, ocispecs.Descriptor{
						MediaType: rootDesc.MediaType,
						Digest:    rootDesc.Digest,
						Size:      rootDesc.Size,
					})
				}
				platDesc, err := pickPlatformChild(ctx, rootProvider, rootDesc, s.Descriptor.Platform, pred.BuilderPlatform())
				if err != nil {
					return errors.Wrapf(err, "pick platform child for %s", m.URI)
				}
				if err := contentutil.CopyChain(ctx, stage, rootProvider, platDesc); err != nil {
					return errors.Wrapf(err, "copy image material chain for %s", m.URI)
				}
				if _, ok := seenImageMfst[platDesc.Digest]; !ok {
					seenImageMfst[platDesc.Digest] = struct{}{}
					imageMfsts = append(imageMfsts, platDesc)
				}
				return nil
			}); err != nil {
				return ocispecs.Descriptor{}, err
			}

		case materialKindHTTP:
			if !req.IncludeMaterials {
				continue
			}
			dgst := resourceDigest(m)
			if dgst == "" {
				return ocispecs.Descriptor{}, errors.Errorf("http material %q has no sha256 digest", m.URI)
			}
			if err := sub.Wrap(fmt.Sprintf("http material %s", m.URI), func() error {
				desc, provider, err := req.Materials.Resolve(ctx, m.URI, dgst)
				if err != nil {
					return errors.Wrapf(err, "resolve http material %s", m.URI)
				}
				if provider == nil {
					return ErrMaterialNotFound(m.URI, dgst.String())
				}
				desc.MediaType = layerMediaTypeHTTP
				if err := stage.copyBlob(ctx, provider, desc); err != nil {
					return errors.Wrapf(err, "copy http material %s", desc.Digest)
				}
				if _, ok := seenLayerDigest[desc.Digest]; !ok {
					seenLayerDigest[desc.Digest] = struct{}{}
					materialsLayers = append(materialsLayers, desc)
				}
				return nil
			}); err != nil {
				return ocispecs.Descriptor{}, err
			}

		case materialKindContainerBlob:
			if !req.IncludeMaterials {
				continue
			}
			dgst := resourceDigest(m)
			if dgst == "" {
				return ocispecs.Descriptor{}, errors.Errorf("container-blob material has no sha256 digest")
			}
			if err := sub.Wrap(fmt.Sprintf("container-blob material %s", dgst), func() error {
				desc, provider, err := req.Materials.Resolve(ctx, m.URI, dgst)
				if err != nil {
					return errors.Wrapf(err, "resolve container-blob material %s", dgst)
				}
				if provider == nil {
					return ErrMaterialNotFound(m.URI, dgst.String())
				}
				if desc.MediaType == "" {
					desc.MediaType = layerMediaTypeContainerBlob
				}
				if err := stage.copyBlob(ctx, provider, desc); err != nil {
					return errors.Wrapf(err, "copy container-blob material %s", desc.Digest)
				}
				if _, ok := seenLayerDigest[desc.Digest]; !ok {
					seenLayerDigest[desc.Digest] = struct{}{}
					materialsLayers = append(materialsLayers, desc)
				}
				return nil
			}); err != nil {
				return ocispecs.Descriptor{}, err
			}

		case materialKindGit:
			// Git packfile snapshotting is not implemented. Dedup so a
			// single URI referenced across many platforms warns once.
			warn.Log(sub, "git:"+m.URI, fmt.Sprintf("git material %q is not included in the snapshot (not yet supported)", m.URI))

		case materialKindUnknown:
			warn.Log(sub, "unknown:"+m.URI, fmt.Sprintf("material with URI %q and no recognised scheme is ignored", m.URI))
		}
	}

	var materialsManifestDesc ocispecs.Descriptor
	if req.IncludeMaterials {
		_, mDesc, mData, err := MaterialsManifest(materialsLayers)
		if err != nil {
			return ocispecs.Descriptor{}, err
		}
		if err := stage.writeRaw(ctx, ocispecs.Descriptor{
			MediaType: ocispecs.MediaTypeImageManifest,
			Digest:    mDesc.Digest,
			Size:      mDesc.Size,
		}, mData); err != nil {
			return ocispecs.Descriptor{}, errors.Wrap(err, "write materials manifest")
		}
		materialsManifestDesc = mDesc
	}

	attestDesc := attestMfst
	// The attestation manifest's descriptor in the original image index
	// carries Docker reference annotations (vnd.docker.reference.*) that
	// are meaningless for the snapshot-subject role — strip them. Preserve
	// the manifest's own artifactType so consumers can tell this is a
	// Docker attestation manifest without fetching the body.
	attestDesc.Annotations = nil
	attestMfstData, err := content.ReadBlob(ctx, stage, attestMfst)
	if err != nil {
		return ocispecs.Descriptor{}, errors.Wrapf(err, "read attestation manifest %s", attestMfst.Digest)
	}
	var attestMfstBody ocispecs.Manifest
	if err := json.Unmarshal(attestMfstData, &attestMfstBody); err != nil {
		return ocispecs.Descriptor{}, errors.Wrapf(err, "parse attestation manifest %s", attestMfst.Digest)
	}
	attestDesc.ArtifactType = attestMfstBody.ArtifactType

	_, ppDesc, ppData, err := PerPlatformSnapshotIndex(attestDesc, materialsManifestDesc, imageMfsts)
	if err != nil {
		return ocispecs.Descriptor{}, err
	}
	if err := stage.writeRaw(ctx, ocispecs.Descriptor{
		MediaType: ocispecs.MediaTypeImageIndex,
		Digest:    ppDesc.Digest,
		Size:      ppDesc.Size,
	}, ppData); err != nil {
		return ocispecs.Descriptor{}, errors.Wrap(err, "write per-platform snapshot index")
	}
	return ppDesc, nil
}

// stagingStore wraps a contentutil.Buffer with a few helpers and also
// tracks every digest written into the buffer so the final flush can emit
// exactly that set (contentutil.Buffer.Walk is a stub, so we maintain our
// own digest list). Any ingester-facing code that bypasses the helpers
// (e.g. contentutil.CopyChain) must be invoked with stagingStore itself —
// it implements content.Ingester — so those writes are tracked too.
type stagingStore struct {
	buffer contentutil.Buffer
	mu     sync.Mutex
	order  []digest.Digest
	descs  map[digest.Digest]ocispecs.Descriptor
}

func newStagingStore() *stagingStore {
	return &stagingStore{
		buffer: contentutil.NewBuffer(),
		descs:  map[digest.Digest]ocispecs.Descriptor{},
	}
}

// record captures the full descriptor for each blob written into the stage.
// Later writes merge fields (e.g. a FetchHandler Writer has the MediaType
// from content.WithDescriptor; writeRaw and copyBlob pass full descriptors).
func (s *stagingStore) record(desc ocispecs.Descriptor) {
	if desc.Digest == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if prev, ok := s.descs[desc.Digest]; ok {
		if prev.MediaType == "" && desc.MediaType != "" {
			prev.MediaType = desc.MediaType
		}
		if prev.Size == 0 && desc.Size > 0 {
			prev.Size = desc.Size
		}
		s.descs[desc.Digest] = prev
		return
	}
	s.descs[desc.Digest] = desc
	s.order = append(s.order, desc.Digest)
}

// ReaderAt satisfies content.Provider, delegating to the wrapped buffer.
func (s *stagingStore) ReaderAt(ctx context.Context, desc ocispecs.Descriptor) (content.ReaderAt, error) {
	return s.buffer.ReaderAt(ctx, desc)
}

// Writer satisfies content.Ingester; commits are tracked so the final dump
// can enumerate exactly what the snapshot added.
func (s *stagingStore) Writer(ctx context.Context, opts ...content.WriterOpt) (content.Writer, error) {
	w, err := s.buffer.Writer(ctx, opts...)
	if err != nil {
		return nil, err
	}
	var desc ocispecs.Descriptor
	var wOpts content.WriterOpts
	for _, o := range opts {
		_ = o(&wOpts)
	}
	desc = wOpts.Desc
	return &stagingWriter{Writer: w, store: s, desc: desc}, nil
}

type stagingWriter struct {
	content.Writer
	store *stagingStore
	desc  ocispecs.Descriptor
}

func (w *stagingWriter) Commit(ctx context.Context, size int64, expected digest.Digest, opts ...content.Opt) error {
	if err := w.Writer.Commit(ctx, size, expected, opts...); err != nil {
		return err
	}
	d := expected
	if d == "" {
		d = w.Digest()
	}
	recorded := w.desc
	recorded.Digest = d
	if recorded.Size == 0 {
		recorded.Size = size
	}
	w.store.record(recorded)
	return nil
}

// writeRaw writes the bytes dt under desc.Digest. Already-present content is
// treated as a no-op (the staging buffer is content-addressable).
func (s *stagingStore) writeRaw(ctx context.Context, desc ocispecs.Descriptor, dt []byte) error {
	if desc.Digest == "" {
		return errors.New("writeRaw: empty digest")
	}
	if desc.Size == 0 {
		desc.Size = int64(len(dt))
	}
	ref := "snapshot-" + desc.Digest.String()
	err := content.WriteBlob(ctx, s, ref, bytes.NewReader(dt), desc)
	if err != nil && !errdefs.IsAlreadyExists(err) {
		return errors.WithStack(err)
	}
	s.record(desc)
	return nil
}

// copyBlob copies one blob from src through into the staging buffer. This is
// a content-only copy — no children / manifest walk.
func (s *stagingStore) copyBlob(ctx context.Context, src content.Provider, desc ocispecs.Descriptor) error {
	if desc.Digest == "" {
		return errors.New("copyBlob: empty digest")
	}
	if err := contentutil.Copy(ctx, s, src, desc, "snapshot-"+desc.Digest.String(), nil); err != nil {
		return err
	}
	s.record(desc)
	return nil
}

// resolveImageMaterial looks up a provenance image material by its recorded
// URI + root digest. Prefers a locally-configured MaterialsResolver (which
// may be snapshot-backed) and falls back to fetching from the registry
// derived from the pkg:docker purl.
func resolveImageMaterial(
	ctx context.Context,
	resolver *MaterialsResolver,
	lazyResolver func() (*imagetools.Resolver, error),
	m slsa1.ResourceDescriptor,
	rootDgst digest.Digest,
	opts ...ResolveOption,
) (ocispecs.Descriptor, content.Provider, error) {
	if resolver != nil {
		desc, provider, err := resolver.Resolve(ctx, m.URI, rootDgst, opts...)
		if err == nil && provider != nil {
			if desc.MediaType == "" {
				desc.MediaType = ocispecs.MediaTypeImageIndex
			}
			return desc, provider, nil
		}
		if err != nil {
			var mnf *MaterialNotFoundError
			if !errors.As(err, &mnf) {
				return ocispecs.Descriptor{}, nil, err
			}
			// Fall through to registry fetch for MaterialNotFoundError.
		}
	}

	// Fall back to a registry fetch driven off the pkg:docker purl.
	ref, _, err := purl.PURLToRef(m.URI)
	if err != nil {
		return ocispecs.Descriptor{}, nil, errors.Wrapf(err, "invalid image material URI %q", m.URI)
	}
	imgResolver, err := lazyResolver()
	if err != nil {
		return ocispecs.Descriptor{}, nil, err
	}
	// Address the root directly by digest so we fetch the exact recorded
	// index regardless of tag mutations since the original build.
	fetchRef := ref
	if !strings.Contains(fetchRef, "@") {
		fetchRef = ref + "@" + rootDgst.String()
	}
	// Resolve the fetchRef first so we learn the root descriptor's size +
	// mediaType. Without a size, contentutil.FromFetcher's ReaderAt reports
	// size=0 and ReadBlob returns an empty payload (silently succeeding with
	// a JSON decode error downstream).
	_, rootDesc, err := imgResolver.Resolve(ctx, fetchRef)
	if err != nil {
		return ocispecs.Descriptor{}, nil, errors.Wrapf(err, "resolve image material %s", m.URI)
	}
	fetcher, err := imgResolver.Fetcher(ctx, fetchRef)
	if err != nil {
		return ocispecs.Descriptor{}, nil, errors.Wrapf(err, "fetch image material %s", m.URI)
	}
	provider := contentutil.FromFetcher(fetcher)
	if rootDesc.Digest == "" {
		rootDesc.Digest = rootDgst
	}
	if rootDesc.MediaType == "" {
		// Paranoia: if the resolver left the mediaType empty, probe by
		// reading the manifest payload.
		dt, err := content.ReadBlob(ctx, provider, rootDesc)
		if err != nil {
			return ocispecs.Descriptor{}, nil, errors.Wrapf(err, "read image material root %s", rootDgst)
		}
		mt, err := detectManifestMediaType(dt)
		if err != nil {
			return ocispecs.Descriptor{}, nil, err
		}
		rootDesc.MediaType = mt
		if rootDesc.Size == 0 {
			rootDesc.Size = int64(len(dt))
		}
	}
	return rootDesc, provider, nil
}

// pickPlatformChild turns a "root" descriptor for an image material into the
// platform-specific manifest descriptor to store in the snapshot. Provenance
// only records the root index digest, so replay has to guess which child
// BuildKit actually resolved. The matcher prefers the subject's platform
// (FROM-style base images resolve at TARGETPLATFORM) and falls back to the
// builder platform recorded on the predicate (frontend images and
// cross-compile toolchains run on the build host). An index with a single
// child is always returned as-is.
func pickPlatformChild(ctx context.Context, provider content.Provider, root ocispecs.Descriptor, subjectPlat *ocispecs.Platform, builderPlat ocispecs.Platform) (ocispecs.Descriptor, error) {
	switch root.MediaType {
	case ocispecs.MediaTypeImageManifest, images.MediaTypeDockerSchema2Manifest:
		return root, nil
	case ocispecs.MediaTypeImageIndex, images.MediaTypeDockerSchema2ManifestList:
		dt, err := content.ReadBlob(ctx, provider, root)
		if err != nil {
			return ocispecs.Descriptor{}, errors.WithStack(err)
		}
		var idx ocispecs.Index
		if err := json.Unmarshal(dt, &idx); err != nil {
			return ocispecs.Descriptor{}, errors.WithStack(err)
		}

		matcher := replayPlatformMatcher(subjectPlat, builderPlat)
		var best *ocispecs.Descriptor
		for i := range idx.Manifests {
			c := idx.Manifests[i]
			if c.Platform == nil || !matcher.Match(*c.Platform) {
				continue
			}
			if best == nil || matcher.Less(*c.Platform, *best.Platform) {
				best = &c
			}
		}
		if best != nil {
			return *best, nil
		}
		if len(idx.Manifests) == 1 {
			return idx.Manifests[0], nil
		}
		return ocispecs.Descriptor{}, errors.Errorf("image material index %s has no child matching subject platform %s or builder %s", root.Digest, formatPlatformPtr(subjectPlat), platforms.Format(builderPlat))
	default:
		return ocispecs.Descriptor{}, errors.Errorf("unsupported image material root media type %q", root.MediaType)
	}
}

// replayPlatformMatcher returns a MatchComparer that prefers the subject's
// platform then falls back to the builder platform recorded on the
// predicate. When the subject has no platform, the builder platform is used
// directly (buildx records builderPlatform as the effective default for any
// subject produced by that build).
func replayPlatformMatcher(subjectPlat *ocispecs.Platform, builderPlat ocispecs.Platform) platforms.MatchComparer {
	if subjectPlat == nil || platforms.Only(*subjectPlat).Match(builderPlat) {
		return platforms.Only(builderPlat)
	}
	return platforms.Any(*subjectPlat, builderPlat)
}

func formatPlatformPtr(p *ocispecs.Platform) string {
	if p == nil {
		return "<unset>"
	}
	return platforms.Format(*p)
}

// snapshotTargetLabel produces a human-readable label for progress output:
// "<platform> (<digest>)" when the subject has a platform, otherwise just
// the digest.
func snapshotTargetLabel(s *Subject) string {
	d := s.Descriptor.Digest.String()
	if s.Descriptor.Platform != nil {
		return platforms.Format(*s.Descriptor.Platform) + " (" + d + ")"
	}
	return d
}

// detectManifestMediaType inspects the first bytes of a manifest blob and
// returns the OCI media type — shared with util/imagetools but re-implemented
// here to avoid importing a single helper.
func detectManifestMediaType(dt []byte) (string, error) {
	var probe struct {
		MediaType string            `json:"mediaType"`
		Manifests []json.RawMessage `json:"manifests,omitempty"`
		Config    json.RawMessage   `json:"config,omitempty"`
	}
	if err := json.Unmarshal(dt, &probe); err != nil {
		return "", errors.WithStack(err)
	}
	if probe.MediaType != "" {
		return probe.MediaType, nil
	}
	if len(probe.Manifests) > 0 {
		return ocispecs.MediaTypeImageIndex, nil
	}
	if len(probe.Config) > 0 {
		return ocispecs.MediaTypeImageManifest, nil
	}
	return "", errors.Errorf("cannot detect media type from manifest payload")
}

// materialKind classifies a provenance material by its URI scheme.
type materialKind int

const (
	materialKindUnknown materialKind = iota
	materialKindImage
	materialKindHTTP
	materialKindContainerBlob
	materialKindGit
)

// classifyMaterial picks a materialKind from a ResourceDescriptor's URI.
func classifyMaterial(m slsa1.ResourceDescriptor) materialKind {
	switch {
	case strings.HasPrefix(m.URI, "pkg:docker/"):
		return materialKindImage
	case strings.HasPrefix(m.URI, "https://") || strings.HasPrefix(m.URI, "http://"):
		if looksLikeGitURL(m.URI) {
			return materialKindGit
		}
		return materialKindHTTP
	case strings.HasPrefix(m.URI, "git+") ||
		strings.HasPrefix(m.URI, "git://") ||
		strings.HasPrefix(m.URI, "ssh://"):
		return materialKindGit
	case m.URI == "" && resourceDigest(m) != "":
		// No URI but a digest: treat as a container-blob material. Layers
		// pulled directly by digest during a build show up this way.
		return materialKindContainerBlob
	}
	return materialKindUnknown
}

// looksLikeGitURL returns true when an http(s) URL appears to name a git repo
// (the provenance recorder sometimes uses a bare https url for a git remote).
func looksLikeGitURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	return strings.HasSuffix(strings.ToLower(u.Path), ".git")
}

// resourceDigest extracts the preferred digest from a SLSA ResourceDescriptor
// (sha256 first, then any other algorithm). Mirrors policy.preferredDigest.
func resourceDigest(m slsa1.ResourceDescriptor) digest.Digest {
	if m.Digest == nil {
		return ""
	}
	if v, ok := m.Digest["sha256"]; ok && v != "" {
		return digest.NewDigestFromEncoded(digest.SHA256, v)
	}
	for alg, v := range m.Digest {
		if v == "" {
			continue
		}
		return digest.NewDigestFromEncoded(digest.Algorithm(alg), v)
	}
	return ""
}

// writeSnapshotOutput materialises the staged snapshot content through the
// selected --output target. oci / registry are supported.
func writeSnapshotOutput(
	ctx context.Context,
	stage *stagingStore,
	root ocispecs.Descriptor,
	exp *buildflags.ExportEntry,
	dockerCli command.Cli,
	builderName string,
) error {
	switch exp.Type {
	case "oci":
		if exp.Destination == "" {
			return errors.New("snapshot: type=oci requires dest=<file|dir|->")
		}
		// tar defaults to true — "tar=false" selects the oci-layout
		// directory form. The TTY refusal for dest=- lives in the
		// command layer; this function trusts its caller.
		if exp.Attrs["tar"] == "false" {
			return writeOCILayoutDir(ctx, stage, root, exp.Destination)
		}
		return writeOCILayoutTar(ctx, stage, root, exp.Destination)
	case "registry":
		ref := exp.Attrs["name"]
		if ref == "" {
			return errors.New("snapshot: type=registry requires name=<ref>")
		}
		return pushSnapshotToRegistry(ctx, stage, root, ref, dockerCli, builderName)
	}
	return errors.Errorf("snapshot: unsupported --output type %q (want oci | registry)", exp.Type)
}

// writeOCILayoutDir writes the snapshot as an OCI layout tree at dest.
// The staging store already contains exactly the content that belongs in
// the snapshot (each target loop staged precisely what was needed), so we
// flush every blob verbatim — no graph walk is required.
func writeOCILayoutDir(ctx context.Context, stage *stagingStore, root ocispecs.Descriptor, dest string) error {
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return errors.WithStack(err)
	}
	store, err := contentlocal.NewStore(dest)
	if err != nil {
		return errors.Wrapf(err, "open store at %s", dest)
	}
	if err := flushStage(ctx, store, stage); err != nil {
		return errors.Wrap(err, "copy snapshot into oci-layout")
	}
	idx := ociindex.NewStoreIndex(dest)
	if err := idx.Put(root, ociindex.Tag("latest")); err != nil {
		return errors.Wrapf(err, "update oci-layout index at %s", dest)
	}
	return nil
}

// flushStage copies every tracked blob from stage into ingester by reading
// bytes directly from the staging buffer and writing them through the
// ingester. Doing it at the byte level avoids containerd's remotes-layer
// ref-key lookup (which warns on the empty mediaType we'd otherwise need
// to plumb through for every blob).
func flushStage(ctx context.Context, ingester content.Ingester, stage *stagingStore) error {
	stage.mu.Lock()
	order := slices.Clone(stage.order)
	stage.mu.Unlock()
	for _, dgst := range order {
		info, err := stage.buffer.Info(ctx, dgst)
		if err != nil {
			return errors.Wrapf(err, "lookup %s", dgst)
		}
		desc := ocispecs.Descriptor{Digest: info.Digest, Size: info.Size}
		ra, err := stage.ReaderAt(ctx, desc)
		if err != nil {
			return errors.Wrapf(err, "read %s", dgst)
		}
		err = content.WriteBlob(ctx, ingester, "snapshot-"+dgst.String(), content.NewReader(ra), desc)
		ra.Close()
		if err != nil && !errdefs.IsAlreadyExists(err) {
			return errors.Wrapf(err, "write %s", dgst)
		}
	}
	return nil
}

// writeOCILayoutTar writes the snapshot as an OCI layout tar at dest.
//
// containerd's imgarchive.Export walks the tree via images.Children, which
// returns only config+layers for manifests and manifests[] for indexes —
// it never follows `subject`. Our per-platform snapshot index reaches the
// attestation manifest only through `subject`, so an imgarchive-driven
// export would leave the attestation chain out of the tar. We instead
// materialise the snapshot into a temp oci-layout directory using our
// own walker (which does follow `subject`) and tar that directory.
func writeOCILayoutTar(ctx context.Context, stage *stagingStore, root ocispecs.Descriptor, dest string) error {
	tmp, err := os.MkdirTemp("", "buildx-snapshot-tar-")
	if err != nil {
		return errors.WithStack(err)
	}
	defer os.RemoveAll(tmp)
	if err := writeOCILayoutDir(ctx, stage, root, tmp); err != nil {
		return err
	}

	var w io.Writer = os.Stdout
	if dest != "-" {
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return errors.WithStack(err)
		}
		f, err := os.Create(dest)
		if err != nil {
			return errors.WithStack(err)
		}
		defer f.Close()
		w = f
	}
	return tarDirectory(tmp, w)
}

// tarDirectory writes the contents of srcDir into w as a tar archive. Entries
// are rooted at the directory's contents (not the directory name itself) so
// the resulting tar matches the OCI image-layout v1 spec: index.json,
// oci-layout and blobs/ at the archive root.
//
// The walk is confined by os.Root so that symlinks or relative components
// inside srcDir cannot escape it and leak unrelated files into the tar.
func tarDirectory(srcDir string, w io.Writer) error {
	root, err := os.OpenRoot(srcDir)
	if err != nil {
		return errors.WithStack(err)
	}
	defer root.Close()

	tw := tar.NewWriter(w)
	defer tw.Close()

	return fs.WalkDir(root.FS(), ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == "." {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return errors.WithStack(err)
		}
		hdr, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return errors.WithStack(err)
		}
		hdr.Name = filepath.ToSlash(path)
		if info.IsDir() {
			hdr.Name += "/"
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return errors.WithStack(err)
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		rf, err := root.Open(path)
		if err != nil {
			return errors.WithStack(err)
		}
		defer rf.Close()
		if _, err := io.Copy(tw, rf); err != nil {
			return errors.WithStack(err)
		}
		return nil
	})
}

// pushSnapshotToRegistry pushes the snapshot to a registry through the buildx
// imagetools.Resolver.
func pushSnapshotToRegistry(ctx context.Context, stage *stagingStore, root ocispecs.Descriptor, ref string, dockerCli command.Cli, builderName string) error {
	loc, err := imagetools.ParseLocation(ref)
	if err != nil {
		return errors.Wrapf(err, "parse registry ref %q", ref)
	}
	if !loc.IsRegistry() {
		return errors.Errorf("snapshot: --output type=registry expects a registry ref, got %q", ref)
	}

	var resolver *imagetools.Resolver
	if dockerCli == nil {
		resolver = imagetools.New(imagetools.Opt{})
	} else {
		b, err := builder.New(dockerCli, builder.WithName(builderName))
		if err != nil {
			return err
		}
		imgOpt, err := b.ImageOpt()
		if err != nil {
			return err
		}
		resolver = imagetools.New(imgOpt)
	}

	ingester, err := resolver.IngesterForLocation(ctx, loc)
	if err != nil {
		return err
	}
	if err := flushStage(ctx, ingester, stage); err != nil {
		return errors.Wrap(err, "copy snapshot to registry")
	}
	rootData, err := content.ReadBlob(ctx, stage, root)
	if err != nil {
		return errors.Wrap(err, "read snapshot root")
	}
	return resolver.Push(ctx, loc, root, rootData)
}
