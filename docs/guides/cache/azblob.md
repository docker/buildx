# Azure Blob Storage cache storage

> **Warning**
>
> This cache backend is unreleased. You can use it today, by using the
> `moby/buildkit:master` image in your Buildx driver.

The `azblob` cache store uploads your resulting build cache to
[Azure's blob storage service](https://azure.microsoft.com/en-us/services/storage/blobs/).

> **Note**
>
> This cache storage backend requires using a different driver than the default
> `docker` driver - see more information on selecting a driver
> [here](../drivers/index.md). To create a new driver (which can act as a simple
> drop-in replacement):
>
> ```console
> docker buildx create --use --driver=docker-container
> ```

## Synopsis

```console
$ docker buildx build . --push -t <registry>/<image> \
  --cache-to type=azblob,name=<cache-image>[,parameters...] \
  --cache-from type=azblob,name=<cache-image>[,parameters...]
```

Common parameters:

- `name`: the name of the cache image.
- `account_url`: the base address of the blob storage account, for example:
  `https://myaccount.blob.core.windows.net`. See
  [authentication](#authentication).
- `secret_access_key`: specifies the
  [Azure Blob Storage account key](https://docs.microsoft.com/en-us/azure/storage/common/storage-account-keys-manage),
  see [authentication](#authentication).

Parameters for `--cache-to`:

- `mode`: specify cache layers to export (default: `min`), see
  [cache mode](./index.md#cache-mode)

## Authentication

The `secret_access_key`, if left unspecified, is read from environment variables
on the BuildKit server following the scheme for the
[Azure Go SDK](https://docs.microsoft.com/en-us/azure/developer/go/azure-sdk-authentication).
The environment variables are read from the server, not the Buildx client.

## Further reading

For an introduction to caching see
[Optimizing builds with cache management](https://docs.docker.com/build/building/cache).

For more information on the `azblob` cache backend, see the
[BuildKit README](https://github.com/moby/buildkit#azure-blob-storage-cache-experimental).
