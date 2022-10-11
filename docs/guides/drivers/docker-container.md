# Docker container driver

The buildx Docker container driver allows creation of a managed and customizable
BuildKit environment in a dedicated Docker container.

Using the Docker container driver has a couple of advantages over the default
Docker driver. For example:

- Specify custom BuildKit versions to use.
- Build multi-arch images, see [QEMU](#qemu)
- Advanced options for
  [cache import and export](https://docs.docker.com/build/building/cache/)

## Synopsis

Run the following command to create a new builder, named `container`, that uses
the Docker container driver:

```console
$ docker buildx create \
  --name container \
  --driver=docker-container \
  --driver-opt=[key=value,...]
container
```

The following table describes the available driver-specific options that you can
pass to `--driver-opt`:

| Parameter       | Value  | Default          | Description                                                                                |
| --------------- | ------ | ---------------- | ------------------------------------------------------------------------------------------ |
| `image`         | String |                  | Sets the image to use for running BuildKit.                                                |
| `network`       | String |                  | Sets the network mode for running the BuildKit container.                                  |
| `cgroup-parent` | String | `/docker/buildx` | Sets the cgroup parent of the BuildKit container if Docker is using the `cgroupfs` driver. |

## Usage

When you run a build, Buildx pulls the specified `image` (by default,
[`moby/buildkit`](https://hub.docker.com/r/moby/buildkit)). When the container
has started, Buildx submits the build submitted to the containerized build
server.

```console
$ docker buildx build -t <image> --builder=container .
WARNING: No output specified with docker-container driver. Build result will only remain in the build cache. To push result image into registry use --push or to load image into docker use --load
#1 [internal] booting buildkit
#1 pulling image moby/buildkit:buildx-stable-1
#1 pulling image moby/buildkit:buildx-stable-1 1.9s done
#1 creating container buildx_buildkit_container0
#1 creating container buildx_buildkit_container0 0.5s done
#1 DONE 2.4s
...
```

## Loading to local image store

Unlike when using the default `docker` driver, images built with the
`docker-container` driver must be explicitly loaded into the local image store.
Use the `--load` flag:

```console
$ docker buildx build --load -t <image> --builder=container .
...
 => exporting to oci image format                                                                                                      7.7s
 => => exporting layers                                                                                                                4.9s
 => => exporting manifest sha256:4e4ca161fa338be2c303445411900ebbc5fc086153a0b846ac12996960b479d3                                      0.0s
 => => exporting config sha256:adf3eec768a14b6e183a1010cb96d91155a82fd722a1091440c88f3747f1f53f                                        0.0s
 => => sending tarball                                                                                                                 2.8s
 => importing to docker
```

The image becomes available in the image store when the build finishes:

```console
$ docker image ls
REPOSITORY                       TAG               IMAGE ID       CREATED             SIZE
<image>                          latest            adf3eec768a1   2 minutes ago       197MB
```

### QEMU

The `docker-container` driver supports using [QEMU](https://www.qemu.org/) (user
mode) to build non-native platforms. Use the `--platform` flag to specify which
architectures that you want to build for.

For example, to build a Linux image for `amd64` and `arm64`:

```console
$ docker buildx build \
  --builder=container \
  --platform=linux/amd64,linux/arm64 \
  -t <registry>/<image> \
  --push .
```

> **Warning**
>
> QEMU performs full-system emulation of non-native platforms, which is much
> slower than native builds. Compute-heavy tasks like compilation and
> compression/decompression will likely take a large performance hit.

## Further reading

For more information on the Docker container driver, see the
[buildx reference](https://docs.docker.com/engine/reference/commandline/buildx_create/#driver).
