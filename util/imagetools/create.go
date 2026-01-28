package imagetools

import (
	"bytes"
	"context"
	"encoding/json"
	"maps"
	"net/url"
	"strings"

	"github.com/containerd/containerd/v2/core/content"
	"github.com/containerd/containerd/v2/core/images"
	"github.com/containerd/containerd/v2/core/remotes"
	"github.com/containerd/errdefs"
	"github.com/containerd/platforms"
	"github.com/distribution/reference"
	"github.com/moby/buildkit/exporter/containerimage/exptypes"
	"github.com/moby/buildkit/util/attestation"
	"github.com/moby/buildkit/util/contentutil"
	"github.com/opencontainers/go-digest"
	"github.com/opencontainers/image-spec/specs-go"
	ocispecs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/pkg/errors"
	"golang.org/x/sync/errgroup"
)

const (
	artifactTypeAttestationManifest = "application/vnd.docker.attestation.manifest.v1+json"
	artifactTypeCosignSignature     = "application/vnd.dev.cosign.artifact.sig.v1+json"
	artifactTypeSigstoreBundle      = "application/vnd.dev.sigstore.bundle.v0.3+json"
)

var supportedArtifactTypes = map[string]struct{}{
	artifactTypeAttestationManifest: {},
	artifactTypeCosignSignature:     {},
	artifactTypeSigstoreBundle:      {},
}

type Source struct {
	Desc ocispecs.Descriptor
	Ref  reference.Named
}

func (r *Resolver) Combine(ctx context.Context, srcs []*Source, ann map[exptypes.AnnotationKey]string, preferIndex bool, platforms []ocispecs.Platform) ([]byte, ocispecs.Descriptor, []DescWithSource, error) {
	dt, desc, srcMap, err := r.combine(ctx, srcs, ann, preferIndex)
	if err != nil {
		return nil, ocispecs.Descriptor{}, nil, err
	}
	dt, desc, mfstsWithSource, err := r.filterPlatforms(ctx, dt, desc, srcMap, platforms)
	if err != nil {
		return nil, ocispecs.Descriptor{}, nil, err
	}
	return dt, desc, mfstsWithSource, nil
}

func (r *Resolver) combine(ctx context.Context, srcs []*Source, ann map[exptypes.AnnotationKey]string, preferIndex bool) ([]byte, ocispecs.Descriptor, map[digest.Digest]*Source, error) {
	eg, ctx := errgroup.WithContext(ctx)

	dts := make([][]byte, len(srcs))
	for i := range dts {
		func(i int) {
			eg.Go(func() error {
				dt, err := r.GetDescriptor(ctx, srcs[i].Ref.String(), srcs[i].Desc)
				if err != nil {
					return err
				}
				dts[i] = dt

				if srcs[i].Desc.MediaType == "" {
					mt, err := detectMediaType(dt)
					if err != nil {
						return err
					}
					srcs[i].Desc.MediaType = mt
				}

				mt := srcs[i].Desc.MediaType

				switch mt {
				case images.MediaTypeDockerSchema2Manifest, ocispecs.MediaTypeImageManifest:
					p := srcs[i].Desc.Platform
					if srcs[i].Desc.Platform == nil {
						p = &ocispecs.Platform{}
					}
					if p.OS == "" || p.Architecture == "" {
						if err := r.loadPlatform(ctx, p, srcs[i].Ref.String(), dt); err != nil {
							return err
						}
					}
					srcs[i].Desc.Platform = p
				case images.MediaTypeDockerSchema1Manifest:
					return errors.Errorf("schema1 manifests are not allowed in manifest lists")
				}

				return nil
			})
		}(i)
	}

	if err := eg.Wait(); err != nil {
		return nil, ocispecs.Descriptor{}, nil, err
	}

	// on single source, return original bytes
	if len(srcs) == 1 && len(ann) == 0 {
		switch srcs[0].Desc.MediaType {
		// if the source is already an image index or manifest list, there is no need to consider the value
		// of preferIndex since if set to true then the source is already in the preferred format, and if false
		// it doesn't matter since we're not going to split it into separate manifests
		case images.MediaTypeDockerSchema2ManifestList, ocispecs.MediaTypeImageIndex:
			srcMap := map[digest.Digest]*Source{
				srcs[0].Desc.Digest: srcs[0],
			}
			return dts[0], srcs[0].Desc, srcMap, nil
		default:
			if !preferIndex {
				return dts[0], srcs[0].Desc, nil, nil
			}
		}
	}

	indexes := map[digest.Digest]int{}
	sources := map[digest.Digest]*Source{}
	descs := make([]ocispecs.Descriptor, 0, len(srcs))

	addDesc := func(d ocispecs.Descriptor, src *Source) {
		idx, ok := indexes[d.Digest]
		if ok {
			old := descs[idx]
			if old.MediaType == "" {
				old.MediaType = d.MediaType
			}
			if d.Platform != nil {
				old.Platform = d.Platform
			}
			if old.Annotations == nil {
				old.Annotations = map[string]string{}
			}
			maps.Copy(old.Annotations, d.Annotations)
			descs[idx] = old
		} else {
			indexes[d.Digest] = len(descs)
			descs = append(descs, d)
		}
		sources[d.Digest] = src
	}

	for i, src := range srcs {
		switch src.Desc.MediaType {
		case images.MediaTypeDockerSchema2ManifestList, ocispecs.MediaTypeImageIndex:
			var mfst ocispecs.Index
			if err := json.Unmarshal(dts[i], &mfst); err != nil {
				return nil, ocispecs.Descriptor{}, nil, errors.WithStack(err)
			}
			for _, d := range mfst.Manifests {
				addDesc(d, src)
			}
		default:
			addDesc(src.Desc, src)
		}
	}

	dockerMfsts := 0
	for _, desc := range descs {
		if strings.HasPrefix(desc.MediaType, "application/vnd.docker.") {
			dockerMfsts++
		}
	}

	var mt string
	if dockerMfsts == len(descs) {
		// all manifests are Docker types, use Docker manifest list
		mt = images.MediaTypeDockerSchema2ManifestList
	} else {
		// otherwise, use OCI index
		mt = ocispecs.MediaTypeImageIndex
	}

	// annotations are only allowed on OCI indexes
	indexAnnotation := make(map[string]string)
	if mt == ocispecs.MediaTypeImageIndex {
		for k, v := range ann {
			switch k.Type {
			case exptypes.AnnotationIndex:
				indexAnnotation[k.Key] = v
			case exptypes.AnnotationManifestDescriptor:
				for i := range descs {
					if descs[i].Annotations == nil {
						descs[i].Annotations = map[string]string{}
					}
					if k.Platform == nil || k.PlatformString() == platforms.Format(*descs[i].Platform) {
						descs[i].Annotations[k.Key] = v
					}
				}
			case exptypes.AnnotationManifest, "":
				return nil, ocispecs.Descriptor{}, nil, errors.Errorf("%q annotations are not supported yet", k.Type)
			case exptypes.AnnotationIndexDescriptor:
				return nil, ocispecs.Descriptor{}, nil, errors.Errorf("%q annotations are invalid while creating an image", k.Type)
			}
		}
	}

	idxBytes, err := json.MarshalIndent(ocispecs.Index{
		MediaType: mt,
		Versioned: specs.Versioned{
			SchemaVersion: 2,
		},
		Manifests:   descs,
		Annotations: indexAnnotation,
	}, "", "  ")
	if err != nil {
		return nil, ocispecs.Descriptor{}, nil, errors.Wrap(err, "failed to marshal index")
	}

	return idxBytes, ocispecs.Descriptor{
		MediaType: mt,
		Size:      int64(len(idxBytes)),
		Digest:    digest.FromBytes(idxBytes),
	}, sources, nil
}

func (r *Resolver) Push(ctx context.Context, ref reference.Named, desc ocispecs.Descriptor, dt []byte) error {
	ctx = remotes.WithMediaTypeKeyPrefix(ctx, "application/vnd.in-toto+json", "intoto")

	fullRef, err := reference.WithDigest(reference.TagNameOnly(ref), desc.Digest)
	if err != nil {
		return errors.Wrapf(err, "failed to combine ref %s with digest %s", ref, desc.Digest)
	}
	p, err := r.resolver().Pusher(ctx, fullRef.String())
	if err != nil {
		return err
	}
	cw, err := p.Push(ctx, desc)
	if err != nil {
		if errdefs.IsAlreadyExists(err) {
			return nil
		}
		return err
	}

	err = content.Copy(ctx, cw, bytes.NewReader(dt), desc.Size, desc.Digest)
	if errdefs.IsAlreadyExists(err) {
		return nil
	}
	return err
}

func (r *Resolver) Copy(ctx context.Context, src *Source, dest reference.Named) error {
	ctx = remotes.WithMediaTypeKeyPrefix(ctx, "application/vnd.in-toto+json", "intoto")
	ctx = remotes.WithMediaTypeKeyPrefix(ctx, "application/vnd.oci.empty.v1+json", "empty")

	// push by digest
	p, err := r.resolver().Pusher(ctx, dest.Name())
	if err != nil {
		return err
	}

	srcRef := reference.TagNameOnly(src.Ref)
	f, err := r.resolver().Fetcher(ctx, srcRef.String())
	if err != nil {
		return err
	}

	refspec := reference.TrimNamed(src.Ref).String()
	u, err := url.Parse("dummy://" + refspec)
	if err != nil {
		return err
	}

	desc := src.Desc
	desc.Annotations = maps.Clone(desc.Annotations)
	if desc.Annotations == nil {
		desc.Annotations = make(map[string]string)
	}

	source, repo := u.Hostname(), strings.TrimPrefix(u.Path, "/")
	desc.Annotations["containerd.io/distribution.source."+source] = repo

	referrersFetcher, ok := f.(remotes.ReferrersFetcher)
	if !ok {
		return errors.Errorf("fetcher for %s does not support referrers", src.Ref.String())
	}

	opts := []contentutil.CopyOption{
		contentutil.WithReferrers(referrersFunc(func(ctx context.Context, desc ocispecs.Descriptor) ([]ocispecs.Descriptor, error) {
			descs, err := referrersFetcher.FetchReferrers(ctx, desc.Digest)
			if err != nil {
				return nil, err
			}
			var filtered []ocispecs.Descriptor
			for _, d := range descs {
				if _, ok := supportedArtifactTypes[d.ArtifactType]; ok {
					filtered = append(filtered, d)
				}
			}
			return filtered, nil
		})),
	}

	err = contentutil.CopyChain(ctx, contentutil.FromPusher(p), contentutil.FromFetcher(f), desc, opts...)
	if err != nil {
		return err
	}
	return nil
}

func (r *Resolver) loadPlatform(ctx context.Context, p2 *ocispecs.Platform, in string, dt []byte) error {
	var manifest ocispecs.Manifest
	if err := json.Unmarshal(dt, &manifest); err != nil {
		return errors.WithStack(err)
	}

	dt, err := r.GetDescriptor(ctx, in, manifest.Config)
	if err != nil {
		return err
	}

	var p ocispecs.Platform
	if err := json.Unmarshal(dt, &p); err != nil {
		return errors.WithStack(err)
	}

	p = platforms.Normalize(p)

	if p2.Architecture == "" {
		p2.Architecture = p.Architecture
		if p2.Variant == "" {
			p2.Variant = p.Variant
		}
	}
	if p2.OS == "" {
		p2.OS = p.OS
	}

	return nil
}

type referrersFunc func(ctx context.Context, desc ocispecs.Descriptor) ([]ocispecs.Descriptor, error)

func (f referrersFunc) Referrers(ctx context.Context, desc ocispecs.Descriptor) ([]ocispecs.Descriptor, error) {
	return f(ctx, desc)
}

type DescWithSource struct {
	ocispecs.Descriptor
	Source *Source
}

func (r *Resolver) filterPlatforms(ctx context.Context, dt []byte, desc ocispecs.Descriptor, srcMap map[digest.Digest]*Source, plats []ocispecs.Platform) ([]byte, ocispecs.Descriptor, []DescWithSource, error) {
	matcher := platforms.Any(plats...)
	if len(plats) == 0 {
		matcher = platforms.All
	}

	if !images.IsIndexType(desc.MediaType) {
		var mfst ocispecs.Manifest
		if err := json.Unmarshal(dt, &mfst); err != nil {
			return nil, ocispecs.Descriptor{}, nil, errors.Wrapf(err, "failed to parse manifest")
		}
		if desc.Platform == nil {
			return nil, ocispecs.Descriptor{}, nil, errors.Errorf("cannot filter platforms from a manifest without platform information")
		}
		if !matcher.Match(*desc.Platform) {
			return nil, ocispecs.Descriptor{}, nil, errors.Errorf("input platform %s does not match any of the provided platforms", platforms.Format(*desc.Platform))
		}
		return dt, desc, nil, nil
	}

	var idx ocispecs.Index
	if err := json.Unmarshal(dt, &idx); err != nil {
		return nil, ocispecs.Descriptor{}, nil, errors.Wrapf(err, "failed to parse index")
	}

	var manifestMap = map[digest.Digest]ocispecs.Descriptor{}
	for _, m := range idx.Manifests {
		manifestMap[m.Digest] = m
	}
	var references = map[digest.Digest]ocispecs.Descriptor{}
	var matchedManifests = map[digest.Digest]struct{}{}
	for _, m := range idx.Manifests {
		if m.Platform == nil || matcher.Match(*m.Platform) {
			matchedManifests[m.Digest] = struct{}{}
		}
		if refType, ok := m.Annotations[attestation.DockerAnnotationReferenceType]; ok && refType == attestation.DockerAnnotationReferenceTypeDefault {
			dgstStr, ok := m.Annotations[attestation.DockerAnnotationReferenceDigest]
			if !ok {
				continue
			}
			dgst, err := digest.Parse(dgstStr)
			if err != nil {
				continue
			}
			subject, ok := manifestMap[dgst]
			if !ok {
				continue
			}
			if subject.Platform == nil || matcher.Match(*subject.Platform) {
				references[m.Digest] = subject
			}
		}
	}

	var mfsts []ocispecs.Descriptor
	var mfstsWithSource []DescWithSource

	for _, m := range idx.Manifests {
		_, isRef := references[m.Digest]
		if isRef || m.Platform == nil || matcher.Match(*m.Platform) {
			src, ok := srcMap[m.Digest]
			if !ok {
				defaultSource, ok := srcMap[desc.Digest]
				if !ok {
					return nil, ocispecs.Descriptor{}, nil, errors.Errorf("internal error: no source found for %s", m.Digest)
				}
				src = defaultSource
			}
			mfsts = append(mfsts, m)
			mfstsWithSource = append(mfstsWithSource, DescWithSource{
				Descriptor: m,
				Source:     src,
			})
		}
	}

	if len(mfsts) == 0 {
		return nil, ocispecs.Descriptor{}, nil, errors.Errorf("none of the manifests match the provided platforms")
	}

	// try to pull in attestation manifest via referrer if one exists
	addedRef := false
	for d := range matchedManifests {
		if _, ok := references[d]; ok { // manifest itself is already attestation
			continue
		}
		hasRef := false
		for _, subject := range references {
			if subject.Digest == d {
				hasRef = true
				break
			}
		}
		if hasRef {
			continue
		}
		src, ok := srcMap[d]
		if !ok {
			defaultSource, ok := srcMap[desc.Digest]
			if !ok {
				return nil, ocispecs.Descriptor{}, nil, errors.Errorf("internal error: no source found for %s", d)
			}
			src = defaultSource
		}
		f, err := r.resolver().Fetcher(ctx, src.Ref.String())
		if err != nil {
			return nil, ocispecs.Descriptor{}, nil, err
		}
		rf, ok := f.(remotes.ReferrersFetcher)
		if !ok {
			return nil, ocispecs.Descriptor{}, nil, errors.Errorf("fetcher for %s does not support referrers", srcMap[d].Ref.String())
		}
		refs, err := rf.FetchReferrers(ctx, d, remotes.WithReferrerArtifactTypes(artifactTypeAttestationManifest))
		if err != nil {
			if errors.Is(err, errdefs.ErrNotFound) {
				continue
			}
			return nil, ocispecs.Descriptor{}, nil, err
		}
		for _, ref := range refs {
			if _, ok := references[ref.Digest]; ok {
				continue
			}
			ref.Platform = &ocispecs.Platform{
				OS: "unknown", Architecture: "unknown",
			}
			if ref.Annotations == nil {
				ref.Annotations = map[string]string{}
			}
			ref.Annotations[attestation.DockerAnnotationReferenceType] = attestation.DockerAnnotationReferenceTypeDefault
			ref.Annotations[attestation.DockerAnnotationReferenceDigest] = d.String()
			ref.ArtifactType = ""
			mfsts = append(mfsts, ref)
			addedRef = true
			break
		}
	}

	if len(mfsts) == len(idx.Manifests) && !addedRef {
		// all platforms matched, no need to rewrite index
		return dt, desc, mfstsWithSource, nil
	}

	idx.Manifests = mfsts
	idxBytes, err := json.MarshalIndent(&idx, "", "  ")
	if err != nil {
		return nil, ocispecs.Descriptor{}, nil, errors.Wrap(err, "failed to marshal index")
	}

	desc = ocispecs.Descriptor{
		MediaType:   desc.MediaType,
		Size:        int64(len(idxBytes)),
		Digest:      digest.FromBytes(idxBytes),
		Annotations: desc.Annotations,
	}

	return idxBytes, desc, mfstsWithSource, nil
}

func detectMediaType(dt []byte) (string, error) {
	var mfst struct {
		MediaType string          `json:"mediaType"`
		Config    json.RawMessage `json:"config"`
		FSLayers  []string        `json:"fsLayers"`
	}

	if err := json.Unmarshal(dt, &mfst); err != nil {
		return "", errors.WithStack(err)
	}

	if mfst.MediaType != "" {
		return mfst.MediaType, nil
	}
	if mfst.Config != nil {
		return images.MediaTypeDockerSchema2Manifest, nil
	}
	if len(mfst.FSLayers) > 0 {
		return images.MediaTypeDockerSchema1Manifest, nil
	}

	return images.MediaTypeDockerSchema2ManifestList, nil
}
