# buildx build

```text
docker buildx build [OPTIONS] PATH | URL | -
```

<!---MARKER_GEN_START-->
Start a build

### Aliases

`docker buildx build`, `docker buildx b`

### Options

| Name                                                                                                                                               | Type          | Default   | Description                                                                                         |
|:---------------------------------------------------------------------------------------------------------------------------------------------------|:--------------|:----------|:----------------------------------------------------------------------------------------------------|
| [`--add-host`](https://docs.docker.com/reference/cli/docker/image/build/#add-host)                                                                 | `stringSlice` |           | Add a custom host-to-IP mapping (format: `host:ip`)                                                 |
| [`--allow`](#allow)                                                                                                                                | `stringSlice` |           | Allow extra privileged entitlement (e.g., `network.host`, `security.insecure`)                      |
| [`--annotation`](#annotation)                                                                                                                      | `stringArray` |           | Add annotation to the image                                                                         |
| [`--attest`](#attest)                                                                                                                              | `stringArray` |           | Attestation parameters (format: `type=sbom,generator=image`)                                        |
| [`--build-arg`](#build-arg)                                                                                                                        | `stringArray` |           | Set build-time variables                                                                            |
| [`--build-context`](#build-context)                                                                                                                | `stringArray` |           | Additional build contexts (e.g., name=path)                                                         |
| [`--builder`](#builder)                                                                                                                            | `string`      |           | Override the configured builder instance                                                            |
| [`--cache-from`](#cache-from)                                                                                                                      | `stringArray` |           | External cache sources (e.g., `user/app:cache`, `type=local,src=path/to/dir`)                       |
| [`--cache-to`](#cache-to)                                                                                                                          | `stringArray` |           | Cache export destinations (e.g., `user/app:cache`, `type=local,dest=path/to/dir`)                   |
| `--call`                                                                                                                                           | `string`      | `build`   | Set method for evaluating build (`check`, `outline`, `targets`)                                     |
| [`--cgroup-parent`](https://docs.docker.com/reference/cli/docker/image/build/#cgroup-parent)                                                       | `string`      |           | Set the parent cgroup for the `RUN` instructions during build                                       |
| `--detach`                                                                                                                                         |               |           | Detach buildx server (supported only on linux) (EXPERIMENTAL)                                       |
| [`-f`](https://docs.docker.com/reference/cli/docker/image/build/#file), [`--file`](https://docs.docker.com/reference/cli/docker/image/build/#file) | `string`      |           | Name of the Dockerfile (default: `PATH/Dockerfile`)                                                 |
| `--iidfile`                                                                                                                                        | `string`      |           | Write the image ID to a file                                                                        |
| `--label`                                                                                                                                          | `stringArray` |           | Set metadata for an image                                                                           |
| [`--load`](#load)                                                                                                                                  |               |           | Shorthand for `--output=type=docker`                                                                |
| [`--metadata-file`](#metadata-file)                                                                                                                | `string`      |           | Write build result metadata to a file                                                               |
| `--network`                                                                                                                                        | `string`      | `default` | Set the networking mode for the `RUN` instructions during build                                     |
| `--no-cache`                                                                                                                                       |               |           | Do not use cache when building the image                                                            |
| [`--no-cache-filter`](#no-cache-filter)                                                                                                            | `stringArray` |           | Do not cache specified stages                                                                       |
| [`-o`](#output), [`--output`](#output)                                                                                                             | `stringArray` |           | Output destination (format: `type=local,dest=path`)                                                 |
| [`--platform`](#platform)                                                                                                                          | `stringArray` |           | Set target platform for build                                                                       |
| [`--progress`](#progress)                                                                                                                          | `string`      | `auto`    | Set type of progress output (`auto`, `plain`, `tty`). Use plain to show container output            |
| [`--provenance`](#provenance)                                                                                                                      | `string`      |           | Shorthand for `--attest=type=provenance`                                                            |
| `--pull`                                                                                                                                           |               |           | Always attempt to pull all referenced images                                                        |
| [`--push`](#push)                                                                                                                                  |               |           | Shorthand for `--output=type=registry`                                                              |
| `-q`, `--quiet`                                                                                                                                    |               |           | Suppress the build output and print image ID on success                                             |
| `--root`                                                                                                                                           | `string`      |           | Specify root directory of server to connect (EXPERIMENTAL)                                          |
| [`--sbom`](#sbom)                                                                                                                                  | `string`      |           | Shorthand for `--attest=type=sbom`                                                                  |
| [`--secret`](#secret)                                                                                                                              | `stringArray` |           | Secret to expose to the build (format: `id=mysecret[,src=/local/secret]`)                           |
| `--server-config`                                                                                                                                  | `string`      |           | Specify buildx server config file (used only when launching new server) (EXPERIMENTAL)              |
| [`--shm-size`](#shm-size)                                                                                                                          | `bytes`       | `0`       | Shared memory size for build containers                                                             |
| [`--ssh`](#ssh)                                                                                                                                    | `stringArray` |           | SSH agent socket or keys to expose to the build (format: `default\|<id>[=<socket>\|<key>[,<key>]]`) |
| [`-t`](https://docs.docker.com/reference/cli/docker/image/build/#tag), [`--tag`](https://docs.docker.com/reference/cli/docker/image/build/#tag)    | `stringArray` |           | Name and optionally a tag (format: `name:tag`)                                                      |
| [`--target`](https://docs.docker.com/reference/cli/docker/image/build/#target)                                                                     | `string`      |           | Set the target build stage to build                                                                 |
| [`--ulimit`](#ulimit)                                                                                                                              | `ulimit`      |           | Ulimit options                                                                                      |


<!---MARKER_GEN_END-->

Flags marked with `[experimental]` need to be explicitly enabled by setting the
`BUILDX_EXPERIMENTAL=1` environment variable.

## Description

The `buildx build` command starts a build using BuildKit. This command is similar
to the UI of `docker build` command and takes the same flags and arguments.

For documentation on most of these flags, refer to the [`docker build`
documentation](https://docs.docker.com/reference/cli/docker/image/build/).
This page describes a subset of the new flags.

## Examples

### <a name="annotation"></a> Create annotations (--annotation)

```text
--annotation="key=value"
--annotation="[type:]key=value"
```

Add OCI annotations to the image index, manifest, or descriptor.
The following example adds the `foo=bar` annotation to the image manifests:

```console
$ docker buildx build -t TAG --annotation "foo=bar" --push .
```

You can optionally add a type prefix to specify the level of the annotation. By
default, the image manifest is annotated. The following example adds the
`foo=bar` annotation the image index instead of the manifests:

```console
$ docker buildx build -t TAG --annotation "index:foo=bar" --push .
```

You can specify multiple types, separated by a comma (,) to add the annotation
to multiple image components. The following example adds the `foo=bar`
annotation to image index, descriptors, manifests:

```console
$ docker buildx build -t TAG --annotation "index,manifest,manifest-descriptor:foo=bar" --push .
```

You can also specify a platform qualifier in square brackets (`[os/arch]`) in
the type prefix, to apply the annotation to a subset of manifests with the
matching platform. The following example adds the `foo=bar` annotation only to
the manifest with the `linux/amd64` platform:

```console
$ docker buildx build -t TAG --annotation "manifest[linux/amd64]:foo=bar" --push .
```

Wildcards are not supported in the platform qualifier; you can't specify a type
prefix like `manifest[linux/*]` to add annotations only to manifests which has
`linux` as the OS platform.

For more information about annotations, see
[Annotations](https://docs.docker.com/build/building/annotations/).

### <a name="attest"></a> Create attestations (--attest)

```text
--attest=type=sbom,...
--attest=type=provenance,...
```

Create [image attestations](https://docs.docker.com/build/attestations/).
BuildKit currently supports:

- `sbom` - Software Bill of Materials.

  Use `--attest=type=sbom` to generate an SBOM for an image at build-time.
  Alternatively, you can use the [`--sbom` shorthand](#sbom).

  For more information, see [here](https://docs.docker.com/build/attestations/sbom/).

- `provenance` - SLSA Provenance

  Use `--attest=type=provenance` to generate provenance for an image at
  build-time. Alternatively, you can use the [`--provenance` shorthand](#provenance).

  By default, a minimal provenance attestation will be created for the build
  result, which will only be attached for images pushed to registries.

  For more information, see [here](https://docs.docker.com/build/attestations/slsa-provenance/).

### <a name="allow"></a> Allow extra privileged entitlement (--allow)

```text
--allow=ENTITLEMENT
```

Allow extra privileged entitlement. List of entitlements:

- `network.host` - Allows executions with host networking.
- `security.insecure` - Allows executions without sandbox. See
  [related Dockerfile extensions](https://docs.docker.com/reference/dockerfile/#run---security).

For entitlements to be enabled, the BuildKit daemon also needs to allow them
with `--allow-insecure-entitlement` (see [`create --buildkitd-flags`](buildx_create.md#buildkitd-flags)).

```console
$ docker buildx create --use --name insecure-builder --buildkitd-flags '--allow-insecure-entitlement security.insecure'
$ docker buildx build --allow security.insecure .
```

### <a name="build-arg"></a> Set build-time variables (--build-arg)

Same as [`docker build` command](https://docs.docker.com/reference/cli/docker/image/build/#build-arg).

There are also useful built-in build arguments, such as:

* `BUILDKIT_CONTEXT_KEEP_GIT_DIR=<bool>`: trigger git context to keep the `.git` directory
* `BUILDKIT_INLINE_CACHE=<bool>`: inline cache metadata to image config or not
* `BUILDKIT_MULTI_PLATFORM=<bool>`: opt into deterministic output regardless of multi-platform output or not

```console
$ docker buildx build --build-arg BUILDKIT_MULTI_PLATFORM=1 .
```

Learn more about the built-in build arguments in the [Dockerfile reference docs](https://docs.docker.com/reference/dockerfile/#buildkit-built-in-build-args).

### <a name="build-context"></a> Additional build contexts (--build-context)

```text
--build-context=name=VALUE
```

Define additional build context with specified contents. In Dockerfile the context can be accessed when `FROM name` or `--from=name` is used.
When Dockerfile defines a stage with the same name it is overwritten.

The value can be a local source directory, [local OCI layout compliant directory](https://github.com/opencontainers/image-spec/blob/main/image-layout.md), container image (with docker-image:// prefix), Git or HTTP URL.

Replace `alpine:latest` with a pinned one:

```console
$ docker buildx build --build-context alpine=docker-image://alpine@sha256:0123456789 .
```

Expose a secondary local source directory:

```console
$ docker buildx build --build-context project=path/to/project/source .
# docker buildx build --build-context project=https://github.com/myuser/project.git .
```

```dockerfile
# syntax=docker/dockerfile:1
FROM alpine
COPY --from=project myfile /
```

#### <a name="source-oci-layout"></a> Use an OCI layout directory as build context

Source an image from a local [OCI layout compliant directory](https://github.com/opencontainers/image-spec/blob/main/image-layout.md),
either by tag, or by digest:

```console
$ docker buildx build --build-context foo=oci-layout:///path/to/local/layout:<tag>
$ docker buildx build --build-context foo=oci-layout:///path/to/local/layout@sha256:<digest>
```

```dockerfile
# syntax=docker/dockerfile:1
FROM alpine
RUN apk add git
COPY --from=foo myfile /

FROM foo
```

The OCI layout directory must be compliant with the [OCI layout specification](https://github.com/opencontainers/image-spec/blob/main/image-layout.md).
You can reference an image in the layout using either tags, or the exact digest.

### <a name="builder"></a> Override the configured builder instance (--builder)

Same as [`buildx --builder`](buildx.md#builder).

### <a name="cache-from"></a> Use an external cache source for a build (--cache-from)

```text
--cache-from=[NAME|type=TYPE[,KEY=VALUE]]
```

Use an external cache source for a build. Supported types are `registry`,
`local`, `gha` and `s3`.

- [`registry` source](https://github.com/moby/buildkit#registry-push-image-and-cache-separately)
  can import cache from a cache manifest or (special) image configuration on the
  registry.
- [`local` source](https://github.com/moby/buildkit#local-directory-1) can
  import cache from local files previously exported with `--cache-to`.
- [`gha` source](https://github.com/moby/buildkit#github-actions-cache-experimental)
  can import cache from a previously exported cache with `--cache-to` in your
  GitHub repository
- [`s3` source](https://github.com/moby/buildkit#s3-cache-experimental)
  can import cache from a previously exported cache with `--cache-to` in your
  S3 bucket

If no type is specified, `registry` exporter is used with a specified reference.

`docker` driver currently only supports importing build cache from the registry.

```console
$ docker buildx build --cache-from=user/app:cache .
$ docker buildx build --cache-from=user/app .
$ docker buildx build --cache-from=type=registry,ref=user/app .
$ docker buildx build --cache-from=type=local,src=path/to/cache .
$ docker buildx build --cache-from=type=gha .
$ docker buildx build --cache-from=type=s3,region=eu-west-1,bucket=mybucket .
```

More info about cache exporters and available attributes: https://github.com/moby/buildkit#export-cache

### <a name="cache-to"></a> Export build cache to an external cache destination (--cache-to)

```text
--cache-to=[NAME|type=TYPE[,KEY=VALUE]]
```

Export build cache to an external cache destination. Supported types are
`registry`, `local`, `inline`, `gha` and `s3`.

- [`registry` type](https://github.com/moby/buildkit#registry-push-image-and-cache-separately) exports build cache to a cache manifest in the registry.
- [`local` type](https://github.com/moby/buildkit#local-directory-1) exports
  cache to a local directory on the client.
- [`inline` type](https://github.com/moby/buildkit#inline-push-image-and-cache-together)
  writes the cache metadata into the image configuration.
- [`gha` type](https://github.com/moby/buildkit#github-actions-cache-experimental)
  exports cache through the [GitHub Actions Cache service API](https://github.com/tonistiigi/go-actions-cache/blob/master/api.md#authentication).
- [`s3` type](https://github.com/moby/buildkit#s3-cache-experimental) exports
  cache to a S3 bucket.

The `docker` driver only supports cache exports using the `inline` and `local`
cache backends.

Attribute key:

- `mode` - Specifies how many layers are exported with the cache. `min` on only
  exports layers already in the final build stage, `max` exports layers for
  all stages. Metadata is always exported for the whole build.

```console
$ docker buildx build --cache-to=user/app:cache .
$ docker buildx build --cache-to=type=inline .
$ docker buildx build --cache-to=type=registry,ref=user/app .
$ docker buildx build --cache-to=type=local,dest=path/to/cache .
$ docker buildx build --cache-to=type=gha .
$ docker buildx build --cache-to=type=s3,region=eu-west-1,bucket=mybucket .
```

More info about cache exporters and available attributes: https://github.com/moby/buildkit#export-cache

### <a name="load"></a> Load the single-platform build result to `docker images` (--load)

Shorthand for [`--output=type=docker`](#docker). Will automatically load the
single-platform build result to `docker images`.

### <a name="metadata-file"></a> Write build result metadata to a file (--metadata-file)

To output build metadata such as the image digest, pass the `--metadata-file` flag.
The metadata will be written as a JSON object to the specified file. The
directory of the specified file must already exist and be writable.

```console
$ docker buildx build --load --metadata-file metadata.json .
$ cat metadata.json
```

```json
{
  "buildx.build.provenance": {},
  "buildx.build.ref": "mybuilder/mybuilder0/0fjb6ubs52xx3vygf6fgdl611",
  "containerimage.config.digest": "sha256:2937f66a9722f7f4a2df583de2f8cb97fc9196059a410e7f00072fc918930e66",
  "containerimage.descriptor": {
    "annotations": {
      "config.digest": "sha256:2937f66a9722f7f4a2df583de2f8cb97fc9196059a410e7f00072fc918930e66",
      "org.opencontainers.image.created": "2022-02-08T21:28:03Z"
    },
    "digest": "sha256:19ffeab6f8bc9293ac2c3fdf94ebe28396254c993aea0b5a542cfb02e0883fa3",
    "mediaType": "application/vnd.oci.image.manifest.v1+json",
    "size": 506
  },
  "containerimage.digest": "sha256:19ffeab6f8bc9293ac2c3fdf94ebe28396254c993aea0b5a542cfb02e0883fa3"
}
```

> **Note**
>
> Build record [provenance](https://docs.docker.com/build/attestations/slsa-provenance/#provenance-attestation-example)
> (`buildx.build.provenance`) includes minimal provenance by default. Set the
> `BUILDX_METADATA_PROVENANCE` environment variable to customize this behavior:
> * `min` sets minimal provenance (default).
> * `max` sets full provenance.
> * `disabled`, `false` or `0` does not set any provenance.

### <a name="no-cache-filter"></a> Ignore build cache for specific stages (--no-cache-filter)

The `--no-cache-filter` lets you specify one or more stages of a multi-stage
Dockerfile for which build cache should be ignored. To specify multiple stages,
use a comma-separated syntax:

```console
$ docker buildx build --no-cache-filter stage1,stage2,stage3 .
```

For example, the following Dockerfile contains four stages:

- `base`
- `install`
- `test`
- `release`

```dockerfile
# syntax=docker/dockerfile:1

FROM oven/bun:1 as base
WORKDIR /app

FROM base AS install
WORKDIR /temp/dev
RUN --mount=type=bind,source=package.json,target=package.json \
    --mount=type=bind,source=bun.lockb,target=bun.lockb \
    bun install --frozen-lockfile

FROM base AS test
COPY --from=install /temp/dev/node_modules node_modules
COPY . .
RUN bun test

FROM base AS release
ENV NODE_ENV=production
COPY --from=install /temp/dev/node_modules node_modules
COPY . .
ENTRYPOINT ["bun", "run", "index.js"]
```

To ignore the cache for the `install` stage:

```console
$ docker buildx build --no-cache-filter install .
```

To ignore the cache the `install` and `release` stages:

```console
$ docker buildx build --no-cache-filter install,release .
```

The arguments for the `--no-cache-filter` flag must be names of stages.

### <a name="output"></a> Set the export action for the build result (-o, --output)

```text
-o, --output=[PATH,-,type=TYPE[,KEY=VALUE]
```

Sets the export action for the build result. In `docker build` all builds finish
by creating a container image and exporting it to `docker images`. `buildx` makes
this step configurable allowing results to be exported directly to the client,
OCI image tarballs, registry etc.

Buildx with `docker` driver currently only supports local, tarball exporter and
image exporter. `docker-container` driver supports all the exporters.

If just the path is specified as a value, `buildx` will use the local exporter
with this path as the destination. If the value is "-", `buildx` will use `tar`
exporter and write to `stdout`.

```console
$ docker buildx build -o . .
$ docker buildx build -o outdir .
$ docker buildx build -o - . > out.tar
$ docker buildx build -o type=docker .
$ docker buildx build -o type=docker,dest=- . > myimage.tar
$ docker buildx build -t tonistiigi/foo -o type=registry
```

> **Note **
>
> Since BuildKit v0.13.0 multiple outputs can be specified by repeating the flag.

Supported exported types are:

#### `local`

The `local` export type writes all result files to a directory on the client. The
new files will be owned by the current user. On multi-platform builds, all results
will be put in subdirectories by their platform.

Attribute key:

- `dest` - destination directory where files will be written

#### `tar`

The `tar` export type writes all result files as a single tarball on the client.
On multi-platform builds all results will be put in subdirectories by their platform.

Attribute key:

- `dest` - destination path where tarball will be written. “-” writes to stdout.

#### `oci`

The `oci` export type writes the result image or manifest list as an [OCI image
layout](https://github.com/opencontainers/image-spec/blob/v1.0.1/image-layout.md)
tarball on the client.

Attribute key:

- `dest` - destination path where tarball will be written. “-” writes to stdout.

#### `docker`

The `docker` export type writes the single-platform result image as a [Docker image
specification](https://github.com/docker/docker/blob/v20.10.2/image/spec/v1.2.md)
tarball on the client. Tarballs created by this exporter are also OCI compatible.

The default image store in Docker Engine doesn't support loading multi-platform
images. You can enable the containerd image store, or push multi-platform images
is to directly push to a registry, see [`registry`](#registry).

Attribute keys:

- `dest` - destination path where tarball will be written. If not specified,
  the tar will be loaded automatically to the local image store.
- `context` - name for the Docker context where to import the result

#### `image`

The `image` exporter writes the build result as an image or a manifest list. When
using `docker` driver the image will appear in `docker images`. Optionally, image
can be automatically pushed to a registry by specifying attributes.

Attribute keys:

- `name` - name (references) for the new image.
- `push` - Boolean to automatically push the image.

#### `registry`

The `registry` exporter is a shortcut for `type=image,push=true`.

### <a name="platform"></a> Set the target platforms for the build (--platform)

```text
--platform=value[,value]
```

Set the target platform for the build. All `FROM` commands inside the Dockerfile
without their own `--platform` flag will pull base images for this platform and
this value will also be the platform of the resulting image.

The default value is the platform of the BuildKit daemon where the build runs.
The value takes the form of `os/arch` or `os/arch/variant`. For example,
`linux/amd64` or `linux/arm/v7`. Additionally, the `--platform` flag also supports
a special `local` value, which tells BuildKit to use the platform of the BuildKit
client that invokes the build.

When using `docker-container` driver with `buildx`, this flag can accept multiple
values as an input separated by a comma. With multiple values the result will be
built for all of the specified platforms and joined together into a single manifest
list.

If the `Dockerfile` needs to invoke the `RUN` command, the builder needs runtime
support for the specified platform. In a clean setup, you can only execute `RUN`
commands for your system architecture.
If your kernel supports [`binfmt_misc`](https://en.wikipedia.org/wiki/Binfmt_misc)
launchers for secondary architectures, buildx will pick them up automatically.
Docker desktop releases come with `binfmt_misc` automatically configured for `arm64`
and `arm` architectures. You can see what runtime platforms your current builder
instance supports by running `docker buildx inspect --bootstrap`.

Inside a `Dockerfile`, you can access the current platform value through
`TARGETPLATFORM` build argument. Refer to the [`docker build`
documentation](https://docs.docker.com/reference/dockerfile/#automatic-platform-args-in-the-global-scope)
for the full description of automatic platform argument variants .

You can find the formatting definition for the platform specifier in the
[containerd source code](https://github.com/containerd/containerd/blob/v1.4.3/platforms/platforms.go#L63).

```console
$ docker buildx build --platform=linux/arm64 .
$ docker buildx build --platform=linux/amd64,linux/arm64,linux/arm/v7 .
$ docker buildx build --platform=darwin .
```

### <a name="progress"></a> Set type of progress output (--progress)

```text
--progress=VALUE
```

Set type of progress output (`auto`, `plain`, `tty`). Use plain to show container
output (default "auto").

> **Note**
>
> You can also use the `BUILDKIT_PROGRESS` environment variable to set its value.

The following example uses `plain` output during the build:

```console
$ docker buildx build --load --progress=plain .

#1 [internal] load build definition from Dockerfile
#1 transferring dockerfile: 227B 0.0s done
#1 DONE 0.1s

#2 [internal] load .dockerignore
#2 transferring context: 129B 0.0s done
#2 DONE 0.0s
...
```

> **Note**
>
> Check also the [`BUILDKIT_COLORS`](https://docs.docker.com/build/building/variables/#buildkit_colors)
> environment variable for modifying the colors of the terminal output.

### <a name="provenance"></a> Create provenance attestations (--provenance)

Shorthand for [`--attest=type=provenance`](#attest), used to configure
provenance attestations for the build result. For example,
`--provenance=mode=max` can be used as an abbreviation for
`--attest=type=provenance,mode=max`.

Additionally, `--provenance` can be used with Boolean values to enable or disable
provenance attestations. For example, `--provenance=false` disables all provenance attestations,
while `--provenance=true` enables all provenance attestations.

By default, a minimal provenance attestation will be created for the build
result. Note that the default image store in Docker Engine doesn't support
attestations. Provenance attestations only persist for images pushed directly
to a registry if you use the default image store. Alternatively, you can switch
to using the containerd image store.

For more information about provenance attestations, see
[here](https://docs.docker.com/build/attestations/slsa-provenance/).

### <a name="push"></a> Push the build result to a registry (--push)

Shorthand for [`--output=type=registry`](#registry). Will automatically push the
build result to registry.

### <a name="sbom"></a> Create SBOM attestations (--sbom)

Shorthand for [`--attest=type=sbom`](#attest), used to configure SBOM
attestations for the build result. For example,
`--sbom=generator=<user>/<generator-image>` can be used as an abbreviation for
`--attest=type=sbom,generator=<user>/<generator-image>`.

Additionally, `--sbom` can be used with Boolean values to enable or disable
SBOM attestations. For example, `--sbom=false` disables all SBOM attestations.

Note that the default image store in Docker Engine doesn't support
attestations. Provenance attestations only persist for images pushed directly
to a registry if you use the default image store. Alternatively, you can switch
to using the containerd image store.

For more information, see [here](https://docs.docker.com/build/attestations/sbom/).

### <a name="secret"></a> Secret to expose to the build (--secret)

```text
--secret=[type=TYPE[,KEY=VALUE]
```

Exposes secrets (authentication credentials, tokens) to the build.
A secret can be mounted into the build using a `RUN --mount=type=secret` mount in the
[Dockerfile](https://docs.docker.com/reference/dockerfile/#run---mounttypesecret).
For more information about how to use build secrets, see
[Build secrets](https://docs.docker.com/build/building/secrets/).

Supported types are:

- [`file`](#file)
- [`env`](#env)

Buildx attempts to detect the `type` automatically if unset.

#### `file`

Attribute keys:

- `id` - ID of the secret. Defaults to base name of the `src` path.
- `src`, `source` - Secret filename. `id` used if unset.

```dockerfile
# syntax=docker/dockerfile:1
FROM python:3
RUN pip install awscli
RUN --mount=type=secret,id=aws,target=/root/.aws/credentials \
  aws s3 cp s3://... ...
```

```console
$ docker buildx build --secret id=aws,src=$HOME/.aws/credentials .
```

#### `env`

Attribute keys:

- `id` - ID of the secret. Defaults to `env` name.
- `env` - Secret environment variable. `id` used if unset, otherwise will look for `src`, `source` if `id` unset.

```dockerfile
# syntax=docker/dockerfile:1
FROM node:alpine
RUN --mount=type=bind,target=. \
  --mount=type=secret,id=SECRET_TOKEN \
  SECRET_TOKEN=$(cat /run/secrets/SECRET_TOKEN) yarn run test
```

```console
$ SECRET_TOKEN=token docker buildx build --secret id=SECRET_TOKEN .
```

### <a name="shm-size"></a> Shared memory size for build containers (--shm-size)

Sets the size of the shared memory allocated for build containers when using
`RUN` instructions.

The format is `<number><unit>`. `number` must be greater than `0`. Unit is
optional and can be `b` (bytes), `k` (kilobytes), `m` (megabytes), or `g`
(gigabytes). If you omit the unit, the system uses bytes.

> **Note**
>
> In most cases, it is recommended to let the builder automatically determine
> the appropriate configurations. Manual adjustments should only be considered
> when specific performance tuning is required for complex build scenarios.

### <a name="ssh"></a> SSH agent socket or keys to expose to the build (--ssh)

```text
--ssh=default|<id>[=<socket>|<key>[,<key>]]
```

This can be useful when some commands in your Dockerfile need specific SSH
authentication (e.g., cloning a private repository).

`--ssh` exposes SSH agent socket or keys to the build and can be used with the
[`RUN --mount=type=ssh` mount](https://docs.docker.com/reference/dockerfile/#run---mounttypessh).

Example to access Gitlab using an SSH agent socket:

```dockerfile
# syntax=docker/dockerfile:1
FROM alpine
RUN apk add --no-cache openssh-client
RUN mkdir -p -m 0700 ~/.ssh && ssh-keyscan gitlab.com >> ~/.ssh/known_hosts
RUN --mount=type=ssh ssh -q -T git@gitlab.com 2>&1 | tee /hello
# "Welcome to GitLab, @GITLAB_USERNAME_ASSOCIATED_WITH_SSHKEY" should be printed here
# with the type of build progress is defined as `plain`.
```

```console
$ eval $(ssh-agent)
$ ssh-add ~/.ssh/id_rsa
(Input your passphrase here)
$ docker buildx build --ssh default=$SSH_AUTH_SOCK .
```

### <a name="ulimit"></a> Set ulimits (--ulimit)

`--ulimit` overrides the default ulimits of build's containers when using `RUN`
instructions and are specified with a soft and hard limit as such:
`<type>=<soft limit>[:<hard limit>]`, for example:

```console
$ docker buildx build --ulimit nofile=1024:1024 .
```

> **Note**
>
> If you don't provide a `hard limit`, the `soft limit` is used
> for both values. If no `ulimits` are set, they're inherited from
> the default `ulimits` set on the daemon.

> **Note**
>
> In most cases, it is recommended to let the builder automatically determine
> the appropriate configurations. Manual adjustments should only be considered
> when specific performance tuning is required for complex build scenarios.
