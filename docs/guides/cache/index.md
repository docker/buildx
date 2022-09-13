# Cache storage backends

To ensure that builds run as quickly as possible, BuildKit automatically caches
the build result in its own internal cache. However, in addition to this simple
cache, BuildKit also supports exporting cache for a specific build to an
external location to easily import into future builds.

This external cache becomes almost essential in CI/CD environments, where there
may be little-to-no persistence between runs, but it's still important to keep
the runtime of image builds as low as possible.

> **Warning**
>
> If you use secrets or credentials inside your build process, then ensure you
> manipulate them using the dedicated [--secret](../../reference/buildx_build.md#secret)
> functionality instead of using manually `COPY`d files or build `ARG`s. Using
> manually managed secrets like this with exported cache could lead to an
> information leak.

Currently, Buildx supports the following cache storage backends:

- `inline` image cache, that embeds the build cache into the image, and is pushed to
  the same location as the main output result - note that this only works for the
  `image` exporter.

  ([guide](./inline.md))

- `registry` image cache, that embeds the build cache into a separate image, and
  pushes to a dedicated location separate from the main output.

  ([guide](./registry.md))

- `local` directory cache, that writes the build cache to a local directory on
  the filesystem.

  ([guide](./local.md))

- `gha` GitHub Actions cache, that uploads the build cache to [GitHub](https://docs.github.com/en/rest/actions/cache)
  (experimental).

  ([guide](./gha.md))

- `s3` AWS cache, that uploads the build cache to an [AWS S3 bucket](https://aws.amazon.com/s3/)
  (unreleased).

  ([guide](./s3.md))

- `azblob` Azure cache, that uploads the build cache to [Azure Blob Storage](https://azure.microsoft.com/en-us/services/storage/blobs/)
  (unreleased).

  ([guide](./azblob.md))

To use any of the above backends, you first need to specify it on build with
the [`--cache-to`](../../reference/buildx_build.md#cache-to) option to export
the cache to your storage backend of choice, then use the [`--cache-from`](../../reference/buildx_build.md#cache-from)
option to import the cache from the storage backend into the current build.
Unlike the local BuildKit cache (which is always enabled), **all** of the cache
storage backends have to be explicitly exported to and then explicitly imported
from. Note that all cache exporters except for the `inline` cache, require
[selecting an alternative Buildx driver](../drivers/index.md).

For example, to perform a cache import and export using the [`registry` cache](./registry.md):

```console
$ docker buildx build --push -t <user>/<image> \
  --cache-to type=registry,ref=<user>/<cache-image> \
  --cache-from type=registry,ref=<user>/<cache-image> .
```

> **Warning**
>
> As a general rule, each cache writes to some location - no location can be
> written to twice, without overwriting the previously cached data. If you want
> to maintain multiple separately scoped caches (e.g. a cache per git branch),
> then ensure that you specify different locations in your cache exporters.

While [currently](https://github.com/moby/buildkit/pull/3024) only a single
cache exporter is supported, you can import from as many remote caches as you
like. For example, a common pattern is to use the cache of both the current
branch as well as the main branch (again using the [`registry` cache](./registry.md)):

```console
$ docker buildx build --push -t <user>/<image> \
  --cache-to type=registry,ref=<user>/<cache-image>:<branch> \
  --cache-from type=registry,ref=<user>/<cache-image>:<branch> \
  --cache-from type=registry,ref=<user>/<cache-image>:master .
```
