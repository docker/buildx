package imagetools

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"sync"

	"github.com/containerd/containerd/v2/core/content"
	"github.com/containerd/containerd/v2/core/remotes"
	"github.com/containerd/containerd/v2/core/remotes/docker"
	contentlocal "github.com/containerd/containerd/v2/plugins/content/local"
	"github.com/containerd/errdefs"
	"github.com/containerd/log"
	"github.com/distribution/reference"
	"github.com/docker/buildx/util/resolver"
	"github.com/docker/buildx/util/resolver/auth"
	"github.com/moby/buildkit/client/ociindex"
	"github.com/moby/buildkit/session/auth/authprovider"
	"github.com/moby/buildkit/util/contentutil"
	"github.com/moby/buildkit/util/tracing"
	digest "github.com/opencontainers/go-digest"
	ocispecs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

type Opt struct {
	Auth           authprovider.AuthConfigProvider
	RegistryConfig map[string]resolver.RegistryConfig
}

type Resolver struct {
	auth         docker.Authorizer
	hosts        docker.RegistryHosts
	buffer       contentutil.Buffer
	localStoreMu sync.Mutex
	localStores  map[string]content.Store
	ociReferrers ociLayoutReferrerRecorder
}

func New(opt Opt) *Resolver {
	dockerAuth := auth.NewDockerAuthorizer(auth.WithAuthProvider(opt.Auth), auth.WithAuthClient(http.DefaultClient))
	auth := &withBearerAuthorizer{
		Authorizer: dockerAuth,
		AuthConfig: opt.Auth,
	}
	return &Resolver{
		auth:        auth,
		hosts:       resolver.NewRegistryConfig(opt.RegistryConfig),
		buffer:      contentutil.NewBuffer(),
		localStores: map[string]content.Store{},
	}
}

func (r *Resolver) registryResolver() remotes.Resolver {
	return docker.NewResolver(docker.ResolverOptions{
		Hosts: func(domain string) ([]docker.RegistryHost, error) {
			res, err := r.hosts(domain)
			if err != nil {
				return nil, err
			}
			for i := range res {
				res[i].Authorizer = r.auth
				res[i].Client = tracing.DefaultClient
			}
			return res, nil
		},
	})
}

func (r *Resolver) Resolve(ctx context.Context, in string) (string, ocispecs.Descriptor, error) {
	// discard containerd logger to avoid printing unnecessary info during image reference resolution.
	// https://github.com/containerd/containerd/blob/1a88cf5242445657258e0c744def5017d7cfb492/remotes/docker/resolver.go#L288
	logger := logrus.New()
	logger.Out = io.Discard
	ctx = log.WithLogger(ctx, logrus.NewEntry(logger))

	loc, err := ParseLocation(in)
	if err != nil {
		return "", ocispecs.Descriptor{}, err
	}
	if loc.IsOCILayout() {
		return r.resolveOCILayout(ctx, loc)
	}

	ref, err := parseRef(in)
	if err != nil {
		return "", ocispecs.Descriptor{}, err
	}
	in, desc, err := r.registryResolver().Resolve(ctx, ref.String())
	if err != nil {
		return "", ocispecs.Descriptor{}, err
	}

	return in, desc, nil
}

func (r *Resolver) Get(ctx context.Context, in string) ([]byte, ocispecs.Descriptor, error) {
	in, desc, err := r.Resolve(ctx, in)
	if err != nil {
		return nil, ocispecs.Descriptor{}, err
	}

	loc, err := ParseLocation(in)
	if err != nil {
		return nil, ocispecs.Descriptor{}, err
	}
	dt, err := r.GetDescriptor(ctx, loc, desc)
	if err != nil {
		return nil, ocispecs.Descriptor{}, err
	}
	return dt, desc, nil
}

func (r *Resolver) GetDescriptor(ctx context.Context, loc *Location, desc ocispecs.Descriptor) ([]byte, error) {
	fetcher, err := r.fetcherForLocation(ctx, loc)
	if err != nil {
		return nil, err
	}

	rc, err := fetcher.Fetch(ctx, desc)
	if err != nil {
		return nil, err
	}

	buf := &bytes.Buffer{}
	_, err = io.Copy(buf, rc)
	rc.Close()
	if err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

func (r *Resolver) Fetcher(ctx context.Context, ref string) (remotes.Fetcher, error) {
	loc, err := ParseLocation(ref)
	if err != nil {
		return nil, err
	}
	return r.fetcherForLocation(ctx, loc)
}

func (r *Resolver) fetcherForLocation(ctx context.Context, loc *Location) (remotes.Fetcher, error) {
	if loc.IsRegistry() {
		return r.registryResolver().Fetcher(ctx, loc.String())
	}
	store, err := r.localStore(loc.OCILayout().Path)
	if err != nil {
		return nil, err
	}
	return &providerFetcher{Provider: store}, nil
}

func (r *Resolver) localStore(path string) (content.Store, error) {
	r.localStoreMu.Lock()
	defer r.localStoreMu.Unlock()

	if store, ok := r.localStores[path]; ok {
		return store, nil
	}

	store, err := contentlocal.NewStore(path)
	if err != nil {
		return nil, err
	}
	r.localStores[path] = store
	return store, nil
}

func (r *Resolver) FetchReferrers(ctx context.Context, loc *Location, dgst digest.Digest, opts ...remotes.FetchReferrersOpt) ([]ocispecs.Descriptor, error) {
	if loc.IsOCILayout() {
		return fetchOCILayoutReferrers(ctx, r.GetDescriptor, loc, dgst, opts...)
	}
	f, err := r.registryResolver().Fetcher(ctx, loc.String())
	if err != nil {
		return nil, err
	}
	rf, ok := f.(remotes.ReferrersFetcher)
	if !ok {
		return nil, errors.Errorf("fetcher for %s does not support referrers", loc.String())
	}
	return rf.FetchReferrers(ctx, dgst, opts...)
}

type providerFetcher struct {
	content.Provider
}

func (f *providerFetcher) Fetch(ctx context.Context, desc ocispecs.Descriptor) (io.ReadCloser, error) {
	ra, err := f.ReaderAt(ctx, desc)
	if err != nil {
		return nil, err
	}
	return &readerAtReadCloser{ReaderAt: ra}, nil
}

type readerAtReadCloser struct {
	content.ReaderAt
	offset int64
}

func (r *readerAtReadCloser) Read(dt []byte) (int, error) {
	n, err := r.ReadAt(dt, r.offset)
	r.offset += int64(n)
	if n > 0 && errors.Is(err, io.EOF) {
		return n, nil
	}
	return n, err
}

func (r *readerAtReadCloser) Close() error {
	return r.ReaderAt.Close()
}

func (r *Resolver) resolveOCILayout(ctx context.Context, loc *Location) (string, ocispecs.Descriptor, error) {
	store, err := r.localStore(loc.OCILayout().Path)
	if err != nil {
		return "", ocispecs.Descriptor{}, err
	}
	if loc.Digest() != "" {
		info, err := store.Info(ctx, loc.Digest())
		if err != nil {
			return "", ocispecs.Descriptor{}, err
		}
		desc := ocispecs.Descriptor{Digest: info.Digest, Size: info.Size}
		dt, err := content.ReadBlob(ctx, store, desc)
		if err != nil {
			return "", ocispecs.Descriptor{}, err
		}
		mt, err := detectMediaType(dt)
		if err != nil {
			return "", ocispecs.Descriptor{}, err
		}
		desc.MediaType = mt
		return loc.String(), desc, nil
	}
	idx := ociindex.NewStoreIndex(loc.OCILayout().Path)
	desc, err := idx.Get(loc.Tag())
	if err != nil {
		return "", ocispecs.Descriptor{}, err
	}
	if desc == nil {
		return "", ocispecs.Descriptor{}, errors.Wrapf(errdefs.ErrNotFound, "reference %s not found", loc.String())
	}
	return loc.String(), *desc, nil
}

func parseRef(s string) (reference.Named, error) {
	ref, err := reference.ParseNormalizedNamed(s)
	if err != nil {
		return nil, err
	}
	ref = reference.TagNameOnly(ref)
	return ref, nil
}
