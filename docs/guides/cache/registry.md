# Registry cache storage

The `registry` cache store can be thought of as the natural extension to the
simple `inline` cache. Unlike the `inline` cache, the `registry` cache is
entirely separate from the image, which allows for more flexible usage -
`registry`-backed cache can do everything that the inline cache can do, and
more:

- It allows for separating the cache and resulting image artifacts so that
  you can distribute your final image without the cache inside.
- It can efficiently cache multi-stage builds in `max` mode, instead of only
  the final stage.
- It works with other exporters for more flexibility, instead of only the
  `image` exporter.

> **Note**
>
> The `registry` cache storage backend requires using a different driver than
> the default `docker` driver - see more information on selecting a driver
> [here](../drivers/index.md). To create a new docker-container driver (which
> can act as a simple drop-in replacement):
>
> ```console
> docker buildx create --use --driver=docker-container
> ```

To import and export your cache using the `registry` storage backend we use the
`--cache-to` and `--cache-from` flags and point it to our desired image target
using the `ref` parameter:

```console
$ docker buildx build --push -t <user>/<image> \
  --cache-to type=registry,ref=<user>/<cache-image> \
  --cache-from type=registry,ref=<user>/<cache-image> .
```

You can choose any valid reference value for `ref`, as long as it's not the
same as the target location that you push your image to. You might choose
different tags (e.g. `foo/bar:latest` and `foo/bar:build-cache`), separate
image names (e.g. `foo/bar` and `foo/bar-cache`), or even different
repositories (e.g. `docker.io/foo/bar` and `ghcr.io/foo/bar`).

If the cache does not exist, then the cache import step will fail, but the
build will continue.

## Cache options

Unlike the simple `inline` cache, the `registry` cache has lots of parameters to
adjust its behavior.

### Cache mode

Build cache can be exported in one of two modes: `min` or `max`, with either
`mode=min` or `mode=max` respectively. For example, to build the cache with
`mode=max`:

```console
$ docker buildx build --push -t <user>/<image> \
  --cache-to type=registry,ref=<user>/<cache-image>,mode=max \
  --cache-from type=registry,ref=<user>/<cache-image> .
```

Note that only `--cache-to` needs to be modified, as `--cache-from` will
automatically extract the relevant parameters from the resulting output.

In `min` cache mode (the default), only layers that are exported into the
resulting image are cached, while in `max` cache mode, all layers are cached,
even those of intermediate steps.

While `min` cache is typically smaller (which speeds up import/export times,
and reduces storage costs), `max` cache is more likely to get more cache hits.
Depending on the complexity and location of your build, you should experiment
with both parameters to get the results.

### Cache compression

Since `registry` cache is exported separately from the main build result, you
can specify separate compression parameters for it (which are similar to the
options provided by the `image` exporter). While the defaults have been
selected to provide a good out-of-the-box experience, you may wish to tweak the
parameters to optimize for storage vs compute costs.

To select the compression algorithm, you can use the `compression=<uncompressed|gzip|estargz|zstd>`
option. For example, to build the cache with `compression=zstd`:

```console
$ docker buildx build --push -t <user>/<image> \
  --cache-to type=registry,ref=<user>/<cache-image>,compression=zstd \
  --cache-from type=registry,ref=<user>/<cache-image> .
```

The `compression-level=<value>` option can be used alongside the `compression`
parameter to choose a compression level for the algorithms which support it
(from 0-9 for `gzip` and `estargz` and 0-22 for `zstd`). As a general rule, the
higher the number, the smaller the resulting file will be, but the longer the
compression will take to run.

The `force-compression=<bool>` option can be enabled with `true` (and disabled
with `false`) to force re-compressing layers that have been imported from a
previous cache if the requested compression algorithm is different from the
previous compression algorithm.

> **Note**
>
> The `gzip` and `estargz` compression methods use the [`compress/gzip` package](https://pkg.go.dev/compress/gzip),
> while `zstd` uses the [`github.com/klauspost/compress/zstd` package](https://github.com/klauspost/compress/tree/master/zstd).

### OCI media types

Like the `image` exporter, the `registry` cache exporter supports creating
images with Docker media types or with OCI media types. To enable OCI Media
types, you can use the `oci-mediatypes` property:

```console
$ docker buildx build --push -t <user>/<image> \
  --cache-to type=registry,ref=<user>/<cache-image>,oci-mediatypes=true \
  --cache-from type=registry,ref=<user>/<cache-image> .
```

This property is only meaningful with the `--cache-to` flag, when fetching
cache, BuildKit will auto-detect the correct media types to use.

<!-- FIXME: link to image exporter guide when it's written -->

## Further reading

For an introduction to caching see [Optimizing builds with cache management](https://docs.docker.com/build/building/cache).

For more information on the `registry` cache backend, see the [BuildKit README](https://github.com/moby/buildkit#registry-push-image-and-cache-separately).
