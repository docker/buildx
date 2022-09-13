# Local cache storage

The `local` cache store is a simple cache option that stores your cache as
files in a local directory on your filesystem (using an [OCI image layout](https://github.com/opencontainers/image-spec/blob/main/image-layout.md)
for the underlying directory structure). It's a good choice if you're just
testing locally, or want the flexibility to manage a shared storage option
yourself, by manually uploading it to a file server, or mounting it over an
NFS volume.

> **Note**
>
> The `local` cache storage backend requires using a different driver than
> the default `docker` driver - see more information on selecting a driver
> [here](../drivers/index.md). To create a new docker-container driver (which
> can act as a simple drop-in replacement):
>
> ```console
> docker buildx create --use --driver=docker-container
> ```

To import and export your cache using the `local` storage backend we use the
`--cache-to` and `--cache-from` flags and point it to our desired local
directory using the `dest` and `src` parameters respectively:

```console
$ docker buildx build --push -t <user>/<image> \
  --cache-to type=local,dest=path/to/local/dir \
  --cache-from type=local,src=path/to/local/dir .
```

If the cache does not exist, then the cache import step will fail, but the
build will continue.

## Cache versioning

If you inspect the cache directory manually, you can see the resulting OCI
image layout:

```console
$ ls cache
blobs  index.json  ingest
$ cat cache/index.json | jq
{
  "schemaVersion": 2,
  "manifests": [
    {
      "mediaType": "application/vnd.oci.image.index.v1+json",
      "digest": "sha256:6982c70595cb91769f61cd1e064cf5f41d5357387bab6b18c0164c5f98c1f707",
      "size": 1560,
      "annotations": {
        "org.opencontainers.image.ref.name": "latest"
      }
    }
  ]
}
```

Similarly to the other cache exporters, the cache is replaced on export, by
replacing the contents of the `index.json` file - however, previous caches will
still be available if the hash of the previous cache image index is known.
These old caches will be kept indefinitely, so the local directory will
continue to grow: see [moby/buildkit#1896](https://github.com/moby/buildkit/issues/1896)
for more information.

When importing cache using `--cache-to`, you can additionally specify the
`digest` parameter to force loading an older version of the cache, for example:

```console
$ docker buildx build --push -t <user>/<image> \
  --cache-to type=local,dest=path/to/local/dir \
  --cache-from type=local,ref=path/to/local/dir,digest=sha256:6982c70595cb91769f61cd1e064cf5f41d5357387bab6b18c0164c5f98c1f707 .
```

## Cache options

The `local` cache has lots of parameters to adjust its behavior.

### Cache mode

See [Registry - Cache mode](./registry.md#cache-mode) for more information.

### Cache compression

See [Registry - Cache compression](./registry.md#cache-compression) for more information.

### OCI media types

See [Registry - OCI Media Types](./registry.md#oci-media-types) for more information.

## Further reading

For an introduction to caching see [Optimizing builds with cache management](https://docs.docker.com/build/building/cache).

For more information on the `local` cache backend, see the [BuildKit README](https://github.com/moby/buildkit#local-directory-1).
