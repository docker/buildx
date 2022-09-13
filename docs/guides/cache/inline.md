# Inline cache storage

The `inline` cache store is the simplest way to get an external cache and is
easy to get started using if you're already building and pushing an image.
However, it doesn't scale as well to multi-stage builds as well as the other
drivers do and it doesn't offer separation between your output artifacts and
your cache output. This means that if you're using a particularly complex build
flow, or not exporting your images directly to a registry, then you may want to
consider the [registry](./registry.md) cache.

To export your cache using `inline` storage, we can pass `type=inline` to the
`--cache-to` option:

```console
$ docker buildx build --push -t <user>/<image> --cache-to type=inline .
```

Alternatively, you can also export inline cache by setting the build-arg
`BUILDKIT_INLINE_CACHE`, instead of using the `--cache-to` flag:

```console
$ docker buildx build --push -t <user>/<image> --arg BUILDKIT_INLINE_CACHE=1 .
```

To import the resulting cache on a future build, we can pass `type=registry` to
`--cache-from` which lets us extract the cache from inside a docker image:

```console
$ docker buildx build --push -t <user>/<image> --cache-from type=registry,ref=<user>/<image> .
```

Most of the time, you'll want to have each build both import and export cache
from the cache store - to do this, specify both `--cache-to` and `--cache-from`:

```console
$ docker buildx build --push -t <user>/<image> \
    --cache-to type=inline \
    --cache-from type=registry,ref=<user>/<image>
```

## Further reading

For an introduction to caching see [Optimizing builds with cache management](https://docs.docker.com/build/building/cache).

For more information on the `inline` cache backend, see the [BuildKit README](https://github.com/moby/buildkit#inline-push-image-and-cache-together).
