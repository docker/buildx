# buildx build

```
docker buildx build [OPTIONS] PATH | URL | -
```

<!---MARKER_GEN_START-->
Start a build

### Aliases

`docker buildx build`, `docker buildx b`

### Options

| Name                                                                                                                                                   | Type          | Default   | Description                                                                                         |
|:-------------------------------------------------------------------------------------------------------------------------------------------------------|:--------------|:----------|:----------------------------------------------------------------------------------------------------|
| [`--add-host`](https://docs.docker.com/engine/reference/commandline/build/#add-host)                                                                   | `stringSlice` |           | Add a custom host-to-IP mapping (format: `host:ip`)                                                 |
| [`--allow`](#allow)                                                                                                                                    | `stringSlice` |           | Allow extra privileged entitlement (e.g., `network.host`, `security.insecure`)                      |
| `--annotation`                                                                                                                                         | `stringArray` |           | Add annotation to the image                                                                         |
| [`--attest`](#attest)                                                                                                                                  | `stringArray` |           | Attestation parameters (format: `type=sbom,generator=image`)                                        |
| [`--build-arg`](#build-arg)                                                                                                                            | `stringArray` |           | Set build-time variables                                                                            |
| [`--build-context`](#build-context)                                                                                                                    | `stringArray` |           | Additional build contexts (e.g., name=path)                                                         |
| [`--builder`](#builder)                                                                                                                                | `string`      |           | Override the configured builder instance                                                            |
| [`--cache-from`](#cache-from)                                                                                                                          | `stringArray` |           | External cache sources (e.g., `user/app:cache`, `type=local,src=path/to/dir`)                       |
| [`--cache-to`](#cache-to)                                                                                                                              | `stringArray` |           | Cache export destinations (e.g., `user/app:cache`, `type=local,dest=path/to/dir`)                   |
| [`--cgroup-parent`](https://docs.docker.com/engine/reference/commandline/build/#cgroup-parent)                                                         | `string`      |           | Set the parent cgroup for the `RUN` instructions during build                                       |
| `--detach`                                                                                                                                             |               |           | Detach buildx server (supported only on linux)                                                      |
| [`-f`](https://docs.docker.com/engine/reference/commandline/build/#file), [`--file`](https://docs.docker.com/engine/reference/commandline/build/#file) | `string`      |           | Name of the Dockerfile (default: `PATH/Dockerfile`)                                                 |
| `--iidfile`                                                                                                                                            | `string`      |           | Write the image ID to the file                                                                      |
| `--invoke`                                                                                                                                             | `string`      |           | Invoke a command after the build                                                                    |
| `--label`                                                                                                                                              | `stringArray` |           | Set metadata for an image                                                                           |
| [`--load`](#load)                                                                                                                                      |               |           | Shorthand for `--output=type=docker`                                                                |
| [`--metadata-file`](#metadata-file)                                                                                                                    | `string`      |           | Write build result metadata to the file                                                             |
| `--network`                                                                                                                                            | `string`      | `default` | Set the networking mode for the `RUN` instructions during build                                     |
| `--no-cache`                                                                                                                                           |               |           | Do not use cache when building the image                                                            |
| `--no-cache-filter`                                                                                                                                    | `stringArray` |           | Do not cache specified stages                                                                       |
| [`-o`](#output), [`--output`](#output)                                                                                                                 | `stringArray` |           | Output destination (format: `type=local,dest=path`)                                                 |
| [`--platform`](#platform)                                                                                                                              | `stringArray` |           | Set target platform for build                                                                       |
| `--print`                                                                                                                                              | `string`      |           | Print result of information request (e.g., outline, targets)                                        |
| [`--progress`](#progress)                                                                                                                              | `string`      | `auto`    | Set type of progress output (`auto`, `plain`, `tty`). Use plain to show container output            |
| [`--provenance`](#provenance)                                                                                                                          | `string`      |           | Shorthand for `--attest=type=provenance`                                                            |
| `--pull`                                                                                                                                               |               |           | Always attempt to pull all referenced images                                                        |
| [`--push`](#push)                                                                                                                                      |               |           | Shorthand for `--output=type=registry`                                                              |
| `-q`, `--quiet`                                                                                                                                        |               |           | Suppress the build output and print image ID on success                                             |
| `--root`                                                                                                                                               | `string`      |           | Specify root directory of server to connect                                                         |
| [`--sbom`](#sbom)                                                                                                                                      | `string`      |           | Shorthand for `--attest=type=sbom`                                                                  |
| [`--secret`](#secret)                                                                                                                                  | `stringArray` |           | Secret to expose to the build (format: `id=mysecret[,src=/local/secret]`)                           |
| `--server-config`                                                                                                                                      | `string`      |           | Specify buildx server config file (used only when launching new server)                             |
| [`--shm-size`](#shm-size)                                                                                                                              | `bytes`       | `0`       | Size of `/dev/shm`                                                                                  |
| [`--ssh`](#ssh)                                                                                                                                        | `stringArray` |           | SSH agent socket or keys to expose to the build (format: `default\|<id>[=<socket>\|<key>[,<key>]]`) |
| [`-t`](https://docs.docker.com/engine/reference/commandline/build/#tag), [`--tag`](https://docs.docker.com/engine/reference/commandline/build/#tag)    | `stringArray` |           | Name and optionally a tag (format: `name:tag`)                                                      |
| [`--target`](https://docs.docker.com/engine/reference/commandline/build/#target)                                                                       | `string`      |           | Set the target build stage to build                                                                 |
| [`--ulimit`](#ulimit)                                                                                                                                  | `ulimit`      |           | Ulimit options                                                                                      |


<!---MARKER_GEN_END-->

Flags marked with `[experimental]` need to be explicitly enabled by setting the
`BUILDX_EXPERIMENTAL=1` environment variable.

## Description

The `buildx build` command starts a build using BuildKit. This command is similar
to the UI of `docker build` command and takes the same flags and arguments.

For documentation on most of these flags, refer to the [`docker build`
documentation](https://docs.docker.com/engine/reference/commandline/build/). In
here we'll document a subset of the new flags.

## Examples

### <a name="attest"></a> Create attestations (--attest)

```
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

```
--allow=ENTITLEMENT
```

Allow extra privileged entitlement. List of entitlements:

- `network.host` - Allows executions with host networking.
- `security.insecure` - Allows executions without sandbox. See
  [related Dockerfile extensions](https://docs.docker.com/engine/reference/builder/#run---securitysandbox).

For entitlements to be enabled, the `buildkitd` daemon also needs to allow them
with `--allow-insecure-entitlement` (see [`create --buildkitd-flags`](buildx_create.md#buildkitd-flags))

**Examples**

```console
$ docker buildx create --use --name insecure-builder --buildkitd-flags '--allow-insecure-entitlement security.insecure'
$ docker buildx build --allow security.insecure .
```

### <a name="build-arg"></a> Set build-time variables (--build-arg)

Same as [`docker build` command](https://docs.docker.com/engine/reference/commandline/build/#build-arg).

There are also useful built-in build args like:

* `BUILDKIT_CONTEXT_KEEP_GIT_DIR=<bool>` trigger git context to keep the `.git` directory
* `BUILDKIT_INLINE_CACHE=<bool>` inline cache metadata to image config or not
* `BUILDKIT_MULTI_PLATFORM=<bool>` opt into deterministic output regardless of multi-platform output or not

```console
$ docker buildx build --build-arg BUILDKIT_MULTI_PLATFORM=1 .
```

> **Note**
>
> More built-in build args can be found in [Dockerfile reference docs](https://docs.docker.com/engine/reference/builder/#buildkit-built-in-build-args).

### <a name="build-context"></a> Additional build contexts (--build-context)

```
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

#### <a name="source-oci-layout"></a> Source image from OCI layout directory

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

```
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

```
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

`docker` driver currently only supports exporting inline cache metadata to image
configuration. Alternatively, `--build-arg BUILDKIT_INLINE_CACHE=1` can be used
to trigger inline cache exporter.

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

### <a name="metadata-file"></a> Write build result metadata to the file (--metadata-file)

To output build metadata such as the image digest, pass the `--metadata-file` flag.
The metadata will be written as a JSON object to the specified file. The
directory of the specified file must already exist and be writable.

```console
$ docker buildx build --load --metadata-file metadata.json .
$ cat metadata.json
```
```json
{
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

### <a name="output"></a> Set the export action for the build result (-o, --output)

```
-o, --output=[PATH,-,type=TYPE[,KEY=VALUE]
```

Sets the export action for the build result. In `docker build` all builds finish
by creating a container image and exporting it to `docker images`. `buildx` makes
this step configurable allowing results to be exported directly to the client,
oci image tarballs, registry etc.

Buildx with `docker` driver currently only supports local, tarball exporter and
image exporter. `docker-container` driver supports all the exporters.

If just the path is specified as a value, `buildx` will use the local exporter
with this path as the destination. If the value is "-", `buildx` will use `tar`
exporter and write to `stdout`.

```console
$ docker buildx build -o . .
$ docker buildx build -o outdir .
$ docker buildx build -o - - > out.tar
$ docker buildx build -o type=docker .
$ docker buildx build -o type=docker,dest=- . > myimage.tar
$ docker buildx build -t tonistiigi/foo -o type=registry
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
using `docker` driver the image will appear in `docker images`. Optionally, image
can be automatically pushed to a registry by specifying attributes.

Attribute keys:

- `name` - name (references) for the new image.
- `push` - boolean to automatically push the image.

#### `registry`

The `registry` exporter is a shortcut for `type=image,push=true`.

### <a name="platform"></a> Set the target platforms for the build (--platform)

```
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
`TARGETPLATFORM` build argument. Please refer to the [`docker build`
documentation](https://docs.docker.com/engine/reference/builder/#automatic-platform-args-in-the-global-scope)
for the full description of automatic platform argument variants .

The formatting for the platform specifier is defined in the [containerd source
code](https://github.com/containerd/containerd/blob/v1.4.3/platforms/platforms.go#L63).

```console
$ docker buildx build --platform=linux/arm64 .
$ docker buildx build --platform=linux/amd64,linux/arm64,linux/arm/v7 .
$ docker buildx build --platform=darwin .
```

### <a name="progress"></a> Set type of progress output (--progress)

```
--progress=VALUE
```

Set type of progress output (auto, plain, tty). Use plain to show container
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
> Check also our [Color output controls guide](https://github.com/docker/buildx/blob/master/docs/guides/color-output.md)
> for modifying the colors that are used to output information to the terminal.

### <a name="provenance"></a> Create provenance attestations (--provenance)

Shorthand for [`--attest=type=provenance`](#attest), used to configure
provenance attestations for the build result. For example,
`--provenance=mode=max` can be used as an abbreviation for
`--attest=type=provenance,mode=max`.

Additionally, `--provenance` can be used with boolean values to broadly enable
or disable provenance attestations. For example, `--provenance=false` can be
used to disable all provenance attestations, while `--provenance=true` can be
used to enable all provenance attestations.

By default, a minimal provenance attestation will be created for the build
result, which will only be attached for images pushed to registries.

For more information, see [here](https://docs.docker.com/build/attestations/slsa-provenance/).

### <a name="push"></a> Push the build result to a registry (--push)

Shorthand for [`--output=type=registry`](#registry). Will automatically push the
build result to registry.

### <a name="sbom"></a> Create SBOM attestations (--sbom)

Shorthand for [`--attest=type=sbom`](#attest), used to configure SBOM
attestations for the build result. For example,
`--sbom=generator=<user>/<generator-image>` can be used as an abbreviation for
`--attest=type=sbom,generator=<user>/<generator-image>`.

Additionally, `--sbom` can be used with boolean values to broadly enable or
disable SBOM attestations. For example, `--sbom=false` can be used to disable
all SBOM attestations.

For more information, see [here](https://docs.docker.com/build/attestations/sbom/).

### <a name="secret"></a> Secret to expose to the build (--secret)

```
--secret=[type=TYPE[,KEY=VALUE]
```

Exposes secret to the build. The secret can be used by the build using
[`RUN --mount=type=secret` mount](https://docs.docker.com/engine/reference/builder/#run---mounttypesecret).

If `type` is unset it will be detected. Supported types are:

#### `file`

Attribute keys:

- `id` - ID of the secret. Defaults to basename of the `src` path.
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

### <a name="shm-size"></a> Size of /dev/shm (--shm-size)

The format is `<number><unit>`. `number` must be greater than `0`. Unit is
optional and can be `b` (bytes), `k` (kilobytes), `m` (megabytes), or `g`
(gigabytes). If you omit the unit, the system uses bytes.

### <a name="ssh"></a> SSH agent socket or keys to expose to the build (--ssh)

```
--ssh=default|<id>[=<socket>|<key>[,<key>]]
```

This can be useful when some commands in your Dockerfile need specific SSH
authentication (e.g., cloning a private repository).

`--ssh` exposes SSH agent socket or keys to the build and can be used with the
[`RUN --mount=type=ssh` mount](https://docs.docker.com/engine/reference/builder/#run---mounttypessh).

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

`--ulimit` is specified with a soft and hard limit as such:
`<type>=<soft limit>[:<hard limit>]`, for example:

```console
$ docker buildx build --ulimit nofile=1024:1024 .
```

> **Note**
>
> If you do not provide a `hard limit`, the `soft limit` is used
> for both values. If no `ulimits` are set, they are inherited from
> the default `ulimits` set on the daemon.
