# Cache storage backends

To ensure fast builds, BuildKit automatically caches the build result in its own
internal cache. Additionally, BuildKit also supports exporting build cache to an
external location, making it possible to import in future builds.

An external cache becomes almost essential in CI/CD build environments. Such
environments usually have little-to-no persistence between runs, but it's still
important to keep the runtime of image builds as low as possible.

> **Warning**
>
> If you use secrets or credentials inside your build process, ensure you
> manipulate them using the dedicated
> [`--secret` option](https://docs.docker.com/engine/reference/commandline/buildx_build/#secret).
> Manually managing secrets using `COPY` or `ARG` could result in leaked
> credentials.

## Backends

Buildx supports the following cache storage backends:

- `inline`: embeds the build cache into the image.

  The inline cache gets pushed to the same location as the main output result.
  Note that this only works for the `image` exporter.

- `registry`: embeds the build cache into a separate image, and pushes to a
  dedicated location separate from the main output.

- `local`: writes the build cache to a local directory on the filesystem.

- `gha`: uploads the build cache to
  [GitHub Actions cache](https://docs.github.com/en/rest/actions/cache) (beta).

- `s3`: uploads the build cache to an
  [AWS S3 bucket](https://aws.amazon.com/s3/) (unreleased).

- `azblob`: uploads the build cache to
  [Azure Blob Storage](https://azure.microsoft.com/en-us/services/storage/blobs/)
  (unreleased).

## Command syntax

To use any of the cache backends, you first need to specify it on build with the
[`--cache-to` option](https://docs.docker.com/engine/reference/commandline/buildx_build/#cache-to)
to export the cache to your storage backend of choice. Then, use the
[`--cache-from` option](https://docs.docker.com/engine/reference/commandline/buildx_build/#cache-from)
to import the cache from the storage backend into the current build. Unlike the
local BuildKit cache (which is always enabled), all of the cache storage
backends must be explicitly exported to, and explicitly imported from. All cache
exporters except for the `inline` cache requires that you
[select an alternative Buildx driver](https://docs.docker.com/build/building/drivers/).

Example `buildx` command using the `registry` backend, using import and export
cache:

```console
$ docker buildx build --push -t <registry>/<image> \
  --cache-to type=registry,ref=<registry>/<cache-image>[,parameters...] \
  --cache-from type=registry,ref=<registry>/<cache-image>[,parameters...] .
```

> **Warning**
>
> As a general rule, each cache writes to some location. No location can be
> written to twice, without overwriting the previously cached data. If you want
> to maintain multiple scoped caches (for example, a cache per Git branch), then
> ensure that you use different locations for exported cache.

## Multiple caches

BuildKit currently only supports
[a single cache exporter](https://github.com/moby/buildkit/pull/3024). But you
can import from as many remote caches as you like. For example, a common pattern
is to use the cache of both the current branch and the main branch. The
following example shows importing cache from multiple locations using the
registry cache backend:

```console
$ docker buildx build --push -t <registry>/<image> \
  --cache-to type=registry,ref=<registry>/<cache-image>:<branch> \
  --cache-from type=registry,ref=<registry>/<cache-image>:<branch> \
  --cache-from type=registry,ref=<registry>/<cache-image>:main .
```

## Configuration options

<!-- FIXME: link to image exporter guide when it's written -->

This section describes some of the configuration options available when
generating cache exports. The options described here are common for at least two
or more backend types. Additionally, the different backend types support
specific parameters as well. See the detailed page about each backend type for
more information about which configuration parameters apply.

The common parameters described here are:

- Cache mode
- Cache compression
- OCI media type

### Cache mode

When generating a cache output, the `--cache-to` argument accepts a `mode`
option for defining which layers to include in the exported cache.

Mode can be set to either of two options: `mode=min` or `mode=max`. For example,
to build the cache with `mode=max` with the registry backend:

```console
$ docker buildx build --push -t <registry>/<image> \
  --cache-to type=registry,ref=<registry>/<cache-image>,mode=max \
  --cache-from type=registry,ref=<registry>/<cache-image> .
```

This option is only set when exporting a cache, using `--cache-to`. When
importing a cache (`--cache-from`) the relevant parameters are automatically
detected.

In `min` cache mode (the default), only layers that are exported into the
resulting image are cached, while in `max` cache mode, all layers are cached,
even those of intermediate steps.

While `min` cache is typically smaller (which speeds up import/export times, and
reduces storage costs), `max` cache is more likely to get more cache hits.
Depending on the complexity and location of your build, you should experiment
with both parameters to find the results that work best for you.

### Cache compression

Since `registry` cache image is a separate export artifact from the main build
result, you can specify separate compression parameters for it. These parameters
are similar to the options provided by the `image` exporter. While the default
values provide a good out-of-the-box experience, you may wish to tweak the
parameters to optimize for storage vs compute costs.

To select the compression algorithm, you can use the
`compression=<uncompressed|gzip|estargz|zstd>` option. For example, to build the
cache with `compression=zstd`:

```console
$ docker buildx build --push -t <registry>/<image> \
  --cache-to type=registry,ref=<registry>/<cache-image>,compression=zstd \
  --cache-from type=registry,ref=<registry>/<cache-image> .
```

Use the `compression-level=<value>` option alongside the `compression` parameter
to choose a compression level for the algorithms which support it:

- 0-9 for `gzip` and `estargz`
- 0-22 for `zstd`

As a general rule, the higher the number, the smaller the resulting file will
be, and the longer the compression will take to run.

Use the `force-compression=true` option to force re-compressing layers imported
from a previous cache, if the requested compression algorithm is different from
the previous compression algorithm.

> **Note**
>
> The `gzip` and `estargz` compression methods use the
> [`compress/gzip` package](https://pkg.go.dev/compress/gzip), while `zstd` uses
> the
> [`github.com/klauspost/compress/zstd` package](https://github.com/klauspost/compress/tree/master/zstd).

### OCI media types

Like the `image` exporter, the `registry` cache exporter supports creating
images with Docker media types or with OCI media types. To export OCI media type
cache, use the `oci-mediatypes` property:

```console
$ docker buildx build --push -t <registry>/<image> \
  --cache-to type=registry,ref=<registry>/<cache-image>,oci-mediatypes=true \
  --cache-from type=registry,ref=<registry>/<cache-image> .
```

This property is only meaningful with the `--cache-to` flag. When fetching
cache, BuildKit will auto-detect the correct media types to use.
