package bundle

import (
	"context"

	"github.com/containerd/containerd/v2/core/content"
	cerrdefs "github.com/containerd/errdefs"
	digest "github.com/opencontainers/go-digest"
	ocispecs "github.com/opencontainers/image-spec/specs-go/v1"
)

type nsFallbackStore struct {
	main content.Store
	fb   content.Store
}

var _ content.Store = &nsFallbackStore{}

func (c *nsFallbackStore) Info(ctx context.Context, dgst digest.Digest) (content.Info, error) {
	info, err := c.main.Info(ctx, dgst)
	if err != nil {
		if cerrdefs.IsNotFound(err) {
			return c.fb.Info(ctx, dgst)
		}
	}
	return info, err
}

func (c *nsFallbackStore) Update(ctx context.Context, info content.Info, fieldpaths ...string) (content.Info, error) {
	return c.main.Update(ctx, info, fieldpaths...)
}

func (c *nsFallbackStore) Walk(ctx context.Context, fn content.WalkFunc, filters ...string) error {
	seen := make(map[digest.Digest]struct{})
	err := c.main.Walk(ctx, func(i content.Info) error {
		seen[i.Digest] = struct{}{}
		return fn(i)
	}, filters...)
	if err != nil {
		return err
	}
	return c.fb.Walk(ctx, func(i content.Info) error {
		if _, ok := seen[i.Digest]; ok {
			return nil
		}
		return fn(i)
	}, filters...)
}

func (c *nsFallbackStore) Delete(ctx context.Context, dgst digest.Digest) error {
	return c.main.Delete(ctx, dgst)
}

func (c *nsFallbackStore) Status(ctx context.Context, ref string) (content.Status, error) {
	return c.main.Status(ctx, ref)
}

func (c *nsFallbackStore) ListStatuses(ctx context.Context, filters ...string) ([]content.Status, error) {
	return c.main.ListStatuses(ctx, filters...)
}

func (c *nsFallbackStore) Abort(ctx context.Context, ref string) error {
	return c.main.Abort(ctx, ref)
}

func (c *nsFallbackStore) ReaderAt(ctx context.Context, desc ocispecs.Descriptor) (content.ReaderAt, error) {
	ra, err := c.main.ReaderAt(ctx, desc)
	if err != nil {
		if cerrdefs.IsNotFound(err) {
			return c.fb.ReaderAt(ctx, desc)
		}
	}
	return ra, err
}

func (c *nsFallbackStore) Writer(ctx context.Context, opts ...content.WriterOpt) (content.Writer, error) {
	return c.main.Writer(ctx, opts...)
}
