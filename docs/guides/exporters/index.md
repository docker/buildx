# Exporters overview

BuildKit exporters allow outputting the results of a build to different
locations.

...

Buildx supports the following exporters:

- `image` / `registry`: exports the build result into a container image.
- `local`: exports the build root filesystem into a local directory.
- `tar`: packs the build root filesystem into a local tarball.
- `oci`: exports the build result as a local
  [OCI image layout](https://github.com/opencontainers/image-spec/blob/v1.0.1/image-layout.md).
- `docker`: exports the build result as a local
  [Docker image specification](https://github.com/docker/docker/blob/v20.10.2/image/spec/v1.2.md).

Each exporter creates outputs designed for different use cases. To build
container images ready to load or push to a registry, you can use the `image`
exporter. Alternatively, you can use the `oci` or `docker` exporters to export
the container image to disk directly, to import or post-process as you want.
Finally, if you just want the output of the final root filesystem, you can use
the `local` or `tar` exporters.

## Command syntax

To use any of the exporters, you need to specify it by either using the
available shorthands, or passing the exporter name and its parameters to the
[`--output`](../../reference/buildx_build.md#output) flag.

To get the full flexibility out of the various exporters buildkit has to offer,
you'll need to use the full form of the `--output` flag. However, for
ease-of-use, buildx also offers various familiar short-hands.

### Pushing images

You may already be familiar with the following `buildx` command syntax using
the `-t`/`--tag` and `--push` shorthands to push the resulting image to a
registry:

```console
$ docker buildx build . --tag <registry>/<image> --push
```

You can replicate this command without the shorthands, using the full
`--output` flag:

```console
$ docker buildx build . --output type=image,name=<registry>/<image>,push=true
```

### Loading images

Images built using the [Docker "default" Buildx driver](../../guides/drivers/docker.md)
will be automatically loaded into the engine. This means you can run them
immediately, and they'll be visible in the `docker images` view.

Images built using any of theh other drivers will not be automatically
loaded, and instead require manually adding the `--load` flag:

```console
$ docker buildx build . --tag <registry>/<image> --load
```

## Multiple exporters

While [currently](https://github.com/moby/buildkit/pull/2760) only a single
exporter is supported, you can perform multiple builds one after another to
export the same content twice - because of caching, as long as nothing changes
inbetween, then all builds after the first should be instantaneous.

For example, using both the [`image` exporter](./image.md) and the
[`local` exporter](./local.md)

```console
$ docker buildx build --output type=image,tag=<user>/<image> .
$ docker buildx build --output type=local,dest=<path/to/output> .
```

## Configuration options

This section describes some of the configuration options available for
exporters. The options described here are common for at least two or more
exporter types. Additionally, the different exporters types support specific
parameters as well. See the detailed page about each exporter for more
information about which configuration parameters apply.

The common parameters described here are:

- [Compression](#compression)
- [OCI media type](#oci-media-type)

### Compression

For all exporters that compress their output, you can configure the exact
compression algorithm and level to use. While the default values provide a good
out-of-the-box experience, you may wish to tweak the parameters to optimize for
storage vs compute costs. Changing the compression parameters can reduce
storage space required, and improve image download times, but will increase
build times.

To select the compression algorithm, you can use the `compression` option. For
example, to build an `image` with `compression=zstd`:

```console
$ docker buildx build \
  --output type=image,name=<registry>/<image>,push=true,compression=zstd .
```

Use the `compression-level=<value>` option alongside the `compression`
parameter to choose a compression level for the algorithms which support it:

- 0-9 for `gzip` and `estargz`
- 0-22 for `zstd`

As a general rule, the higher the number, the smaller the resulting file will
be, and the longer the compression will take to run.

Use the `force-compression=true` option to force re-compressing layers imported
from a previous image, if the requested compression algorithm is different from
the previous compression algorithm.

> **Note**
>
> The `gzip` and `estargz` compression methods use the
> [`compress/gzip` package](https://pkg.go.dev/compress/gzip), while `zstd` uses
> the
> [`github.com/klauspost/compress/zstd` package](https://github.com/klauspost/compress/tree/master/zstd).

### OCI media types

Exporters that output container images, support creating images with either
Docker media types or with OCI media types. By default, BuildKit exports
images using Docker image types.

To export images with OCI mediatypes set, use the `oci-mediatypes` property. For
example, with the `image` exporter:

```console
$ docker buildx build \
  --output type=image,name=<registry>/<image>,push=true,oci-mediatypes=true .
```

### Build info

Exporters that output container images, allow embedding information about the
build, including information on the original build request and sources used
during the build.

This build info is attached to the image configuration:

```json
{
  "moby.buildkit.buildinfo.v0": "<base64>"
}
```

By default, the build dependencies are inlined in the image configuration. You
can disable this behavior using the `buildinfo` attribute.

## What's next

Read about each of the exporters to learn about how they work and how to
use them:

- [`image`](./image.md) / [`registry`](./image.md)
- [`local`](./local.md)
- [`tar`](./tar.md)
- [`oci`](./oci.md)
- [`docker`](./docker.md)
