# buildx build

```
Usage:  docker buildx build [OPTIONS] PATH | URL | -

Start a build

Aliases:
  build, b

Options:
      --add-host strings         Add a custom host-to-IP mapping (host:ip)
      --allow strings            Allow extra privileged entitlement, e.g. network.host, security.insecure
      --build-arg stringArray    Set build-time variables
      --builder string           Override the configured builder instance
      --cache-from stringArray   External cache sources (eg. user/app:cache, type=local,src=path/to/dir)
      --cache-to stringArray     Cache export destinations (eg. user/app:cache, type=local,dest=path/to/dir)
  -f, --file string              Name of the Dockerfile (Default is 'PATH/Dockerfile')
      --iidfile string           Write the image ID to the file
      --label stringArray        Set metadata for an image
      --load                     Shorthand for --output=type=docker
      --network string           Set the networking mode for the RUN instructions during build (default "default")
      --no-cache                 Do not use cache when building the image
  -o, --output stringArray       Output destination (format: type=local,dest=path)
      --platform stringArray     Set target platform for build
      --progress string          Set type of progress output (auto, plain, tty). Use plain to show container output (default "auto")
      --pull                     Always attempt to pull a newer version of the image
      --push                     Shorthand for --output=type=registry
      --secret stringArray       Secret file to expose to the build: id=mysecret,src=/local/secret
      --ssh stringArray          SSH agent socket or keys to expose to the build (format: default|<id>[=<socket>|<key>[,<key>]])
  -t, --tag stringArray          Name and optionally a tag in the 'name:tag' format
      --target string            Set the target build stage to build.
```

## Description

The `buildx build` command starts a build using BuildKit. This command is similar
to the UI of `docker build` command and takes the same flags and arguments.

For documentation on most of these flags refer to the [`docker build`
documentation](https://docs.docker.com/engine/reference/commandline/build/). In
here we’ll document a subset of the new flags.

### `--platform=value[,value]`

Set the target platform for the build. All `FROM` commands inside the Dockerfile
without their own `--platform` flag will pull base images for this platform and
this value will also be the platform of the resulting image. The default value
will be the current platform of the buildkit daemon.

When using `docker-container` driver with `buildx`, this flag can accept multiple
values as an input separated by a comma. With multiple values the result will be
built for all of the specified platforms and joined together into a single manifest
list.

If the`Dockerfile` needs to invoke the `RUN` command, the builder needs runtime
support for the specified platform. In a clean setup, you can only execute `RUN`
commands for your system architecture.
If your kernel supports [`binfmt_misc`](https://en.wikipedia.org/wiki/Binfmt_misc)
launchers for secondary architectures buildx will pick them up automatically.
Docker desktop releases come with binfmt_misc automatically configured for `arm64`
and `arm` architectures. You can see what runtime platforms your current builder
instance supports by running `docker buildx inspect --bootstrap`.

Inside a `Dockerfile`, you can access the current platform value through
`TARGETPLATFORM` build argument. Please refer to the [`docker build`
documentation](https://docs.docker.com/engine/reference/builder/#automatic-platform-args-in-the-global-scope)
for the full description of automatic platform argument variants .

The formatting for the platform specifier is defined in the [containerd source
code](https://github.com/containerd/containerd/blob/v1.4.3/platforms/platforms.go#L63).

Examples:

```console
docker buildx build --platform=linux/arm64 .
docker buildx build --platform=linux/amd64,linux/arm64,linux/arm/v7 .
docker buildx build --platform=darwin .
```

### `-o, --output=[PATH,-,type=TYPE[,KEY=VALUE]`

Sets the export action for the build result. In `docker build` all builds finish
by creating a container image and exporting it to `docker images`. `buildx` makes
this step configurable allowing results to be exported directly to the client,
oci image tarballs, registry etc.

Buildx with `docker` driver currently only supports local, tarball exporter and
image exporter. `docker-container` driver supports all the exporters.

If just the path is specified as a value, `buildx` will use the local exporter
with this path as the destination. If the value is "-", `buildx` will use `tar`
exporter and write to `stdout`.

Examples:

```console
docker buildx build -o . .
docker buildx build -o outdir .
docker buildx build -o - - > out.tar
docker buildx build -o type=docker .
docker buildx build -o type=docker,dest=- . > myimage.tar
docker buildx build -t tonistiigi/foo -o type=registry
```

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

Currently, multi-platform images cannot be exported with the `docker` export type.
The most common usecase for multi-platform images is to directly push to a registry
(see [`registry`](#registry)).

Attribute keys:

- `dest` - destination path where tarball will be written. If not specified the
tar will be loaded automatically to the current docker instance.
- `context` - name for the docker context where to import the result

#### `image`

The `image` exporter writes the build result as an image or a manifest list. When
using `docker` driver the image will appear in `docker images`. Optionally image
can be automatically pushed to a registry by specifying attributes.

Attribute keys:

- `name` - name (references) for the new image.
- `push` - boolean to automatically push the image.

#### `registry`

The `registry` exporter is a shortcut for `type=image,push=true`.


### `--push`

Shorthand for [`--output=type=registry`](#registry). Will automatically push the
build result to registry.

### `--load`

Shorthand for [`--output=type=docker`](#docker). Will automatically load the
single-platform build result to `docker images`.

### `--cache-from=[NAME|type=TYPE[,KEY=VALUE]]`

Use an external cache source for a build. Supported types are `registry` and `local`.
The `registry` source can import cache from a cache manifest or (special) image
configuration on the registry. The `local` source can import cache from local
files previously exported with `--cache-to`.

If no type is specified, `registry` exporter is used with a specified reference.

`docker` driver currently only supports importing build cache from the registry.

Examples:

```console
docker buildx build --cache-from=user/app:cache .
docker buildx build --cache-from=user/app .
docker buildx build --cache-from=type=registry,ref=user/app .
docker buildx build --cache-from=type=local,src=path/to/cache .
```

### `--cache-to=[NAME|type=TYPE[,KEY=VALUE]]`

Export build cache to an external cache destination. Supported types are `registry`,
`local` and `inline`. Registry exports build cache to a cache manifest in the
registry, local exports cache to a local directory on the client and inline writes
the cache metadata into the image configuration.

`docker` driver currently only supports exporting inline cache metadata to image
configuration. Alternatively, `--build-arg BUILDKIT_INLINE_CACHE=1` can be used
to trigger inline cache exporter.

Attribute key:

- `mode` - Specifies how many layers are exported with the cache. “min” on only
exports layers already in the final build stage, “max” exports layers for
all stages. Metadata is always exported for the whole build.

Examples:

```console
docker buildx build --cache-to=user/app:cache .
docker buildx build --cache-to=type=inline .
docker buildx build --cache-to=type=registry,ref=user/app .
docker buildx build --cache-to=type=local,dest=path/to/cache .
```

### `--allow=ENTITLEMENT`

Allow extra privileged entitlement. List of entitlements:

- `network.host` - Allows executions with host networking.
- `security.insecure` - Allows executions without sandbox. See
[related Dockerfile extensions](https://github.com/moby/buildkit/blob/master/frontend/dockerfile/docs/experimental.md#run---securityinsecuresandbox).

For entitlements to be enabled, the `buildkitd` daemon also needs to allow them
with `--allow-insecure-entitlement` (see [`create --buildkitd-flags`](buildx_create.md#--buildkitd-flags-flags))

Examples:

```console
$ docker buildx create --use --name insecure-builder --buildkitd-flags '--allow-insecure-entitlement security.insecure'
$ docker buildx build --allow security.insecure .
```
