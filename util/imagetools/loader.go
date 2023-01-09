package imagetools

// TODO: replace with go-imageinspect library when public

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"sync"

	"github.com/containerd/containerd/content"
	"github.com/containerd/containerd/images"
	"github.com/containerd/containerd/platforms"
	"github.com/containerd/containerd/remotes"
	"github.com/docker/distribution/reference"
	"github.com/moby/buildkit/util/contentutil"
	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/pkg/errors"
	"golang.org/x/sync/errgroup"
)

const (
	annotationReference = "vnd.docker.reference.digest"
)

type contentCache interface {
	content.Provider
	content.Ingester
}

type loader struct {
	resolver remotes.Resolver
	cache    contentCache
}

type manifest struct {
	desc     ocispec.Descriptor
	manifest ocispec.Manifest
}

type index struct {
	desc  ocispec.Descriptor
	index ocispec.Index
}

type asset struct {
	config *ocispec.Image
	sbom   *sbomStub
	slsa   *slsaStub
}

type result struct {
	mu        sync.Mutex
	indexes   map[digest.Digest]index
	manifests map[digest.Digest]manifest
	images    map[string]digest.Digest
	refs      map[digest.Digest][]digest.Digest

	platforms []string
	assets    map[string]asset
}

func newLoader(resolver remotes.Resolver) *loader {
	return &loader{
		resolver: resolver,
		cache:    contentutil.NewBuffer(),
	}
}

func (l *loader) Load(ctx context.Context, ref string) (*result, error) {
	named, err := parseRef(ref)
	if err != nil {
		return nil, err
	}

	_, desc, err := l.resolver.Resolve(ctx, named.String())
	if err != nil {
		return nil, err
	}

	canonical, err := reference.WithDigest(named, desc.Digest)
	if err != nil {
		return nil, err
	}

	fetcher, err := l.resolver.Fetcher(ctx, canonical.String())
	if err != nil {
		return nil, err
	}

	r := &result{
		indexes:   make(map[digest.Digest]index),
		manifests: make(map[digest.Digest]manifest),
		images:    make(map[string]digest.Digest),
		refs:      make(map[digest.Digest][]digest.Digest),
		assets:    make(map[string]asset),
	}

	if err := l.fetch(ctx, fetcher, desc, r); err != nil {
		return nil, err
	}

	for platform, dgst := range r.images {
		r.platforms = append(r.platforms, platform)

		mfst, ok := r.manifests[dgst]
		if !ok {
			return nil, errors.Errorf("image %s not found", platform)
		}

		var a asset
		annotations := make(map[string]string, len(mfst.manifest.Annotations)+len(mfst.desc.Annotations))
		for k, v := range mfst.desc.Annotations {
			annotations[k] = v
		}
		for k, v := range mfst.manifest.Annotations {
			annotations[k] = v
		}

		if err := l.scanConfig(ctx, fetcher, mfst.manifest.Config, &a); err != nil {
			return nil, err
		}

		refs, ok := r.refs[dgst]
		if ok {
			if err := l.scanSBOM(ctx, fetcher, r, refs, &a); err != nil {
				return nil, err
			}
		}

		if err := l.scanProvenance(ctx, fetcher, r, refs, &a); err != nil {
			return nil, err
		}

		r.assets[platform] = a
	}

	sort.Strings(r.platforms)
	return r, nil
}

func (l *loader) fetch(ctx context.Context, fetcher remotes.Fetcher, desc ocispec.Descriptor, r *result) error {
	_, err := remotes.FetchHandler(l.cache, fetcher)(ctx, desc)
	if err != nil {
		return err
	}

	switch desc.MediaType {
	case images.MediaTypeDockerSchema2Manifest, ocispec.MediaTypeImageManifest:
		var mfst ocispec.Manifest
		dt, err := content.ReadBlob(ctx, l.cache, desc)
		if err != nil {
			return err
		}
		if err := json.Unmarshal(dt, &mfst); err != nil {
			return err
		}
		r.mu.Lock()
		r.manifests[desc.Digest] = manifest{
			desc:     desc,
			manifest: mfst,
		}
		r.mu.Unlock()

		ref, ok := desc.Annotations[annotationReference]
		if ok {
			refdgst, err := digest.Parse(ref)
			if err != nil {
				return err
			}
			r.mu.Lock()
			r.refs[refdgst] = append(r.refs[refdgst], desc.Digest)
			r.mu.Unlock()
		} else {
			p := desc.Platform
			if p == nil {
				p, err = l.readPlatformFromConfig(ctx, fetcher, mfst.Config)
				if err != nil {
					return err
				}
			}
			r.mu.Lock()
			r.images[platforms.Format(platforms.Normalize(*p))] = desc.Digest
			r.mu.Unlock()
		}
	case images.MediaTypeDockerSchema2ManifestList, ocispec.MediaTypeImageIndex:
		var idx ocispec.Index
		dt, err := content.ReadBlob(ctx, l.cache, desc)
		if err != nil {
			return err
		}

		if err := json.Unmarshal(dt, &idx); err != nil {
			return err
		}

		r.mu.Lock()
		r.indexes[desc.Digest] = index{
			desc:  desc,
			index: idx,
		}
		r.mu.Unlock()

		eg, ctx := errgroup.WithContext(ctx)
		for _, d := range idx.Manifests {
			d := d
			eg.Go(func() error {
				return l.fetch(ctx, fetcher, d, r)
			})
		}

		if err := eg.Wait(); err != nil {
			return err
		}
	default:
	}
	return nil
}

func (l *loader) readPlatformFromConfig(ctx context.Context, fetcher remotes.Fetcher, desc ocispec.Descriptor) (*ocispec.Platform, error) {
	_, err := remotes.FetchHandler(l.cache, fetcher)(ctx, desc)
	if err != nil {
		return nil, err
	}

	dt, err := content.ReadBlob(ctx, l.cache, desc)
	if err != nil {
		return nil, err
	}

	var config ocispec.Image
	if err := json.Unmarshal(dt, &config); err != nil {
		return nil, err
	}

	return &ocispec.Platform{
		OS:           config.OS,
		Architecture: config.Architecture,
		Variant:      config.Variant,
	}, nil
}

func (l *loader) scanConfig(ctx context.Context, fetcher remotes.Fetcher, desc ocispec.Descriptor, as *asset) error {
	_, err := remotes.FetchHandler(l.cache, fetcher)(ctx, desc)
	if err != nil {
		return err
	}
	dt, err := content.ReadBlob(ctx, l.cache, desc)
	if err != nil {
		return err
	}
	return json.Unmarshal(dt, &as.config)
}

type sbomStub struct {
	SPDX json.RawMessage `json:",omitempty"`
}

func (l *loader) scanSBOM(ctx context.Context, fetcher remotes.Fetcher, r *result, refs []digest.Digest, as *asset) error {
	ctx = remotes.WithMediaTypeKeyPrefix(ctx, "application/vnd.in-toto+json", "intoto")
	for _, dgst := range refs {
		mfst, ok := r.manifests[dgst]
		if !ok {
			return errors.Errorf("referenced image %s not found", dgst)
		}
		for _, layer := range mfst.manifest.Layers {
			if layer.MediaType == "application/vnd.in-toto+json" && layer.Annotations["in-toto.io/predicate-type"] == "https://spdx.dev/Document" {
				_, err := remotes.FetchHandler(l.cache, fetcher)(ctx, layer)
				if err != nil {
					return err
				}
				dt, err := content.ReadBlob(ctx, l.cache, layer)
				if err != nil {
					return err
				}
				as.sbom = &sbomStub{
					SPDX: dt,
				}
				break
			}
		}
	}
	return nil
}

type slsaStub struct {
	Provenance json.RawMessage `json:",omitempty"`
}

func (l *loader) scanProvenance(ctx context.Context, fetcher remotes.Fetcher, r *result, refs []digest.Digest, as *asset) error {
	ctx = remotes.WithMediaTypeKeyPrefix(ctx, "application/vnd.in-toto+json", "intoto")
	for _, dgst := range refs {
		mfst, ok := r.manifests[dgst]
		if !ok {
			return errors.Errorf("referenced image %s not found", dgst)
		}
		for _, layer := range mfst.manifest.Layers {
			if layer.MediaType == "application/vnd.in-toto+json" && strings.HasPrefix(layer.Annotations["in-toto.io/predicate-type"], "https://slsa.dev/provenance/") {
				_, err := remotes.FetchHandler(l.cache, fetcher)(ctx, layer)
				if err != nil {
					return err
				}
				dt, err := content.ReadBlob(ctx, l.cache, layer)
				if err != nil {
					return err
				}
				as.slsa = &slsaStub{
					Provenance: dt,
				}
				break
			}
		}
	}
	return nil
}

func (r *result) Configs() map[string]*ocispec.Image {
	if len(r.assets) == 0 {
		return nil
	}
	res := make(map[string]*ocispec.Image)
	for p, a := range r.assets {
		if a.config == nil {
			continue
		}
		res[p] = a.config
	}
	return res
}

func (r *result) SLSA() map[string]slsaStub {
	if len(r.assets) == 0 {
		return nil
	}
	res := make(map[string]slsaStub)
	for p, a := range r.assets {
		if a.slsa == nil {
			continue
		}
		res[p] = *a.slsa
	}
	return res
}

func (r *result) SBOM() map[string]sbomStub {
	if len(r.assets) == 0 {
		return nil
	}
	res := make(map[string]sbomStub)
	for p, a := range r.assets {
		if a.sbom == nil {
			continue
		}
		res[p] = *a.sbom
	}
	return res
}
