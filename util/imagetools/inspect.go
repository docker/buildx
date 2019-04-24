package imagetools

import (
	"bytes"
	"context"
	"io"
	"net/http"

	"github.com/containerd/containerd/remotes"
	"github.com/containerd/containerd/remotes/docker"
	clitypes "github.com/docker/cli/cli/config/types"
	"github.com/docker/distribution/reference"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

type Auth interface {
	GetAuthConfig(registryHostname string) (clitypes.AuthConfig, error)
}

type Opt struct {
	Auth Auth
}

type Resolver struct {
	r remotes.Resolver
}

func New(opt Opt) *Resolver {
	resolver := docker.NewResolver(docker.ResolverOptions{
		Client:      http.DefaultClient,
		Credentials: toCredentialsFunc(opt.Auth),
	})
	return &Resolver{
		r: resolver,
	}
}

func (r *Resolver) Resolve(ctx context.Context, in string) (string, ocispec.Descriptor, error) {
	ref, err := parseRef(in)
	if err != nil {
		return "", ocispec.Descriptor{}, err
	}

	in, desc, err := r.r.Resolve(ctx, ref.String())
	if err != nil {
		return "", ocispec.Descriptor{}, err
	}

	return in, desc, nil
}

func (r *Resolver) Get(ctx context.Context, in string) ([]byte, ocispec.Descriptor, error) {
	in, desc, err := r.Resolve(ctx, in)
	if err != nil {
		return nil, ocispec.Descriptor{}, err
	}

	dt, err := r.GetDescriptor(ctx, in, desc)
	if err != nil {
		return nil, ocispec.Descriptor{}, err
	}
	return dt, desc, nil
}

func (r *Resolver) GetDescriptor(ctx context.Context, in string, desc ocispec.Descriptor) ([]byte, error) {
	fetcher, err := r.r.Fetcher(ctx, in)
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

func parseRef(s string) (reference.Named, error) {
	ref, err := reference.ParseNormalizedNamed(s)
	if err != nil {
		return nil, err
	}
	ref = reference.TagNameOnly(ref)
	return ref, nil
}

func toCredentialsFunc(a Auth) func(string) (string, string, error) {
	return func(host string) (string, string, error) {
		if host == "registry-1.docker.io" {
			host = "https://index.docker.io/v1/"
		}
		ac, err := a.GetAuthConfig(host)
		if err != nil {
			return "", "", err
		}
		if ac.IdentityToken != "" {
			return "", ac.IdentityToken, nil
		}
		return ac.Username, ac.Password, nil
	}
}
