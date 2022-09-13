# Azure Blob Storage cache storage

> **Warning**
>
> The `azblob` cache is currently unreleased. You can use it today, by using
> the `moby/buildkit:master` image in your Buildx driver.

The `azblob` cache store uploads your resulting build cache to
[Azure's blob storage service](https://azure.microsoft.com/en-us/services/storage/blobs/).

> **Note**
>
> The `azblob` cache storage backend requires using a different driver than
> the default `docker` driver - see more information on selecting a driver
> [here](../drivers/index.md). To create a new docker-container driver (which
> can act as a simple drop-in replacement):
>
> ```console
> docker buildx create --use --driver=docker-container
> ```

To import and export your cache using the `azblob` storage backend we use the
`--cache-to` and `--cache-from` flags and point it to our desired blob using
the required `account_url` and `name` parameters:

```console
$ docker buildx build --push -t <user>/<image> \
  --cache-to type=azblob,account_url=https://myaccount.blob.core.windows.net,name=my_image \
  --cache-from type=azblob,account_url=https://myaccount.blob.core.windows.net,name=my_image
```

## Authentication

To authenticate to Azure to read from and write to the cache, the following
parameters are required:

* `secret_access_key`: secret access key
  * specifies the primary or secondary account key for your Azure Blob
    Storage account. [Azure Blob Storage account keys](https://docs.microsoft.com/en-us/azure/storage/common/storage-account-keys-manage)

While these can be manually provided, if left unspecified, then the credentials
for Azure will be pulled from the BuildKit server's environment following the
environment variables scheme for the [Azure Go SDK](https://docs.microsoft.com/en-us/azure/developer/go/azure-sdk-authentication).

> **Warning**
>
> These environment variables **must** be specified on the BuildKit server, not
> the `buildx` client.

## Cache options

The `azblob` cache has lots of parameters to adjust its behavior.

### Cache mode

See [Registry - Cache mode](./registry.md#cache-mode) for more information.

## Further reading

For an introduction to caching see [Optimizing builds with cache management](https://docs.docker.com/build/building/cache).

For more information on the `azblob` cache backend, see the [BuildKit README](https://github.com/moby/buildkit#azure-blob-storage-cache-experimental).
