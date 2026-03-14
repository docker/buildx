package imagetools

import (
	"context"
	"encoding/json"
	"slices"
	"sync"

	"github.com/containerd/containerd/v2/core/images"
	"github.com/containerd/containerd/v2/core/remotes"
	"github.com/containerd/errdefs"
	"github.com/moby/buildkit/client/ociindex"
	"github.com/moby/buildkit/util/attestation"
	"github.com/opencontainers/go-digest"
	ocispecs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/pkg/errors"
)

type ociLayoutReferrerRecorder struct {
	mu   sync.Mutex
	refs map[string]map[digest.Digest][]ocispecs.Descriptor
}

func (r *ociLayoutReferrerRecorder) record(path string, subject digest.Digest, descs []ocispecs.Descriptor) {
	if len(descs) == 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.refs == nil {
		r.refs = map[string]map[digest.Digest][]ocispecs.Descriptor{}
	}
	if r.refs[path] == nil {
		r.refs[path] = map[digest.Digest][]ocispecs.Descriptor{}
	}
	r.refs[path][subject] = dedupeDescriptors(append(r.refs[path][subject], descs...))
}

func (r *ociLayoutReferrerRecorder) take(path string) map[digest.Digest][]ocispecs.Descriptor {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.refs == nil {
		return nil
	}
	out := r.refs[path]
	delete(r.refs, path)
	return out
}

func hasSubjectAnnotation(desc ocispecs.Descriptor) bool {
	return desc.Annotations[images.AnnotationManifestSubject] != ""
}

// fetchOCILayoutReferrers resolves referrers for a subject from an OCI layout by
// combining directly indexed subject entries with referrers reachable from the
// regular named roots in index.json.
func fetchOCILayoutReferrers(ctx context.Context, getDescriptor func(context.Context, *Location, ocispecs.Descriptor) ([]byte, error), loc *Location, subject digest.Digest, opts ...remotes.FetchReferrersOpt) ([]ocispecs.Descriptor, error) {
	idx, err := ociindex.NewStoreIndex(loc.OCILayout().Path).Read()
	if err != nil {
		return nil, err
	}

	out := map[digest.Digest]ocispecs.Descriptor{}
	visited := map[digest.Digest]struct{}{}
	for _, desc := range idx.Manifests {
		if hasSubjectAnnotation(desc) {
			continue
		}
		if err := collectReachableOCILayoutReferrers(ctx, getDescriptor, loc, desc, subject, visited, out); err != nil {
			return nil, err
		}
	}
	for _, desc := range idx.Manifests {
		if desc.Annotations[images.AnnotationManifestSubject] == subject.String() {
			out[desc.Digest] = desc
		}
	}

	if len(out) == 0 {
		return nil, errors.WithStack(errdefs.ErrNotFound)
	}

	refs := make([]ocispecs.Descriptor, 0, len(out))
	for _, desc := range out {
		refs = append(refs, desc)
	}
	return filterOCILayoutReferrers(ctx, refs, opts...)
}

func filterOCILayoutReferrers(ctx context.Context, refs []ocispecs.Descriptor, opts ...remotes.FetchReferrersOpt) ([]ocispecs.Descriptor, error) {
	var cfg remotes.FetchReferrersConfig
	for _, opt := range opts {
		if err := opt(ctx, &cfg); err != nil {
			return nil, err
		}
	}
	if len(cfg.ArtifactTypes) == 0 {
		return refs, nil
	}
	out := make([]ocispecs.Descriptor, 0, len(refs))
	for _, ref := range refs {
		if slices.Contains(cfg.ArtifactTypes, ref.ArtifactType) {
			out = append(out, ref)
		}
	}
	return out, nil
}

// collectReachableOCILayoutReferrers walks a regular OCI layout root and records
// referrer manifests for the requested subject that are already reachable from it.
func collectReachableOCILayoutReferrers(ctx context.Context, getDescriptor func(context.Context, *Location, ocispecs.Descriptor) ([]byte, error), loc *Location, desc ocispecs.Descriptor, subject digest.Digest, visited map[digest.Digest]struct{}, out map[digest.Digest]ocispecs.Descriptor) error {
	if _, ok := visited[desc.Digest]; ok {
		return nil
	}
	visited[desc.Digest] = struct{}{}

	if desc.Annotations[attestation.DockerAnnotationReferenceDigest] == subject.String() {
		out[desc.Digest] = desc
	}

	switch desc.MediaType {
	case ocispecs.MediaTypeImageIndex:
		dt, err := getDescriptor(ctx, loc, desc)
		if err != nil {
			return err
		}
		var idx ocispecs.Index
		if err := json.Unmarshal(dt, &idx); err != nil {
			return errors.WithStack(err)
		}
		for _, child := range idx.Manifests {
			if err := collectReachableOCILayoutReferrers(ctx, getDescriptor, loc, child, subject, visited, out); err != nil {
				return err
			}
		}
	case ocispecs.MediaTypeImageManifest:
		dt, err := getDescriptor(ctx, loc, desc)
		if err != nil {
			return err
		}
		var mfst ocispecs.Manifest
		if err := json.Unmarshal(dt, &mfst); err != nil {
			return errors.WithStack(err)
		}
		if mfst.Subject != nil && mfst.Subject.Digest == subject {
			out[desc.Digest] = desc
		}
	}

	return nil
}

// writePendingOCILayoutReferrers adds copied referrers to index.json only when
// they are not already reachable from the regular top-level roots.
func writePendingOCILayoutReferrers(
	ctx context.Context,
	pending map[digest.Digest][]ocispecs.Descriptor,
	getDescriptor func(context.Context, *Location, ocispecs.Descriptor) ([]byte, error),
	idx ociindex.StoreIndex,
	loc *Location,
) error {
	if len(pending) == 0 {
		return nil
	}
	current, err := idx.Read()
	if err != nil {
		return err
	}

	reachable := map[digest.Digest]struct{}{}
	visited := map[digest.Digest]struct{}{}
	for _, desc := range current.Manifests {
		if err := collectReachableDigests(ctx, getDescriptor, loc, desc, visited, reachable); err != nil {
			return err
		}
	}

	for subject, manifests := range pending {
		for _, desc := range manifests {
			if err := putSubjectReferrerIndexEntry(idx, reachable, subject, desc); err != nil {
				return err
			}
		}
	}
	return nil
}

// collectReachableDigests records descriptors reachable from regular OCI layout
// roots so standalone subject-indexed referrers are not duplicated in index.json.
func collectReachableDigests(ctx context.Context, getDescriptor func(context.Context, *Location, ocispecs.Descriptor) ([]byte, error), loc *Location, desc ocispecs.Descriptor, visited map[digest.Digest]struct{}, reachable map[digest.Digest]struct{}) error {
	if hasSubjectAnnotation(desc) {
		return nil
	}
	if _, ok := visited[desc.Digest]; ok {
		return nil
	}
	visited[desc.Digest] = struct{}{}
	reachable[desc.Digest] = struct{}{}

	if desc.MediaType != ocispecs.MediaTypeImageIndex {
		return nil
	}

	dt, err := getDescriptor(ctx, loc, desc)
	if err != nil {
		return err
	}
	var idx ocispecs.Index
	if err := json.Unmarshal(dt, &idx); err != nil {
		return errors.WithStack(err)
	}
	for _, child := range idx.Manifests {
		if err := collectReachableDigests(ctx, getDescriptor, loc, child, visited, reachable); err != nil {
			return err
		}
	}
	return nil
}

func putSubjectReferrerIndexEntry(idx ociindex.StoreIndex, reachable map[digest.Digest]struct{}, subject digest.Digest, desc ocispecs.Descriptor) error {
	if _, ok := reachable[desc.Digest]; ok {
		return nil
	}
	if desc.Annotations == nil {
		desc.Annotations = map[string]string{}
	}
	if !hasSubjectAnnotation(desc) {
		desc.Annotations[images.AnnotationManifestSubject] = subject.String()
	}
	return idx.Put(desc)
}
