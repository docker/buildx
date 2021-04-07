# buildx

[![PkgGoDev](https://img.shields.io/badge/go.dev-docs-007d9c?logo=go&logoColor=white)](https://pkg.go.dev/github.com/docker/buildx)
[![Build Status](https://github.com/docker/buildx/workflows/build/badge.svg)](https://github.com/docker/buildx/actions?query=workflow%3Abuild)
[![Go Report Card](https://goreportcard.com/badge/github.com/docker/buildx)](https://goreportcard.com/report/github.com/docker/buildx)
[![codecov](https://codecov.io/gh/docker/buildx/branch/master/graph/badge.svg)](https://codecov.io/gh/docker/buildx)

`buildx` is a Docker CLI plugin for extended build capabilities with [BuildKit](https://github.com/moby/buildkit).

Key features:

- Familiar UI from `docker build`
- Full BuildKit capabilities with container driver
- Multiple builder instance support
- Multi-node builds for cross-platform images
- Compose build support
- WIP: High-level build constructs (`bake`)
- In-container driver support (both Docker and Kubernetes)

# Table of Contents

- [Installing](#installing)
- [Building](#building)
    + [with Docker 18.09+](#with-docker-1809)
    + [with buildx or Docker 19.03](#with-buildx-or-docker-1903)
- [Getting started](#getting-started)
  * [Building with buildx](#building-with-buildx)
  * [Working with builder instances](#working-with-builder-instances)
  * [Building multi-platform images](#building-multi-platform-images)
  * [High-level build options](#high-level-build-options)
- [Documentation](#documentation)
    + [`buildx build [OPTIONS] PATH | URL | -`](docs/reference/buildx_build.md)
    + [`buildx create [OPTIONS] [CONTEXT|ENDPOINT]`](docs/reference/buildx_create.md)
    + [`buildx use NAME`](docs/reference/buildx_use.md)
    + [`buildx inspect [NAME]`](docs/reference/buildx_inspect.md)
    + [`buildx ls`](docs/reference/buildx_ls.md)
    + [`buildx stop [NAME]`](docs/reference/buildx_stop.md)
    + [`buildx rm [NAME]`](docs/reference/buildx_rm.md)
    + [`buildx bake [OPTIONS] [TARGET...]`](docs/reference/buildx_bake.md)
    + [`buildx imagetools create [OPTIONS] [SOURCE] [SOURCE...]`](docs/reference/buildx_imagetools_create.md)
    + [`buildx imagetools inspect NAME`](docs/reference/buildx_imagetools_inspect.md)
- [Setting buildx as default builder in Docker 19.03+](#setting-buildx-as-default-builder-in-docker-1903)
- [Contributing](#contributing)


# Installing

Using `buildx` as a docker CLI plugin requires using Docker 19.03. A limited set of functionality works with older versions of Docker when invoking the binary directly.

### Docker CE

`buildx` comes bundled with Docker CE starting with 19.03, but requires experimental mode to be enabled on the Docker CLI.
To enable it, `"experimental": "enabled"` can be added to the CLI configuration file `~/.docker/config.json`. An alternative is to set the `DOCKER_CLI_EXPERIMENTAL=enabled` environment variable.

### Binary release

Download the latest binary release from https://github.com/docker/buildx/releases/latest and copy it to `~/.docker/cli-plugins` folder with name `docker-buildx`.

Change the permission to execute:
```sh
chmod a+x ~/.docker/cli-plugins/docker-buildx
```

# Building

### with Docker 18.09+
```
$ git clone git://github.com/docker/buildx && cd buildx
$ make install
```

### with buildx or Docker 19.03
```
$ export DOCKER_BUILDKIT=1
$ docker build --platform=local -o . git://github.com/docker/buildx
$ mkdir -p ~/.docker/cli-plugins
$ mv buildx ~/.docker/cli-plugins/docker-buildx
```

# Getting started

## Building with buildx

Buildx is a Docker CLI plugin that extends the `docker build` command with the full support of the features provided by [Moby BuildKit](https://github.com/moby/buildkit) builder toolkit. It provides the same user experience as `docker build` with many new features like creating scoped builder instances and building against multiple nodes concurrently.

After installation, buildx can be accessed through the `docker buildx` command with Docker 19.03.  `docker buildx build` is the command for starting a new build. With Docker versions older than 19.03 buildx binary can be called directly to access the `docker buildx` subcommands.

```
$ docker buildx build .
[+] Building 8.4s (23/32)
 => ...
```


Buildx will always build using the BuildKit engine and does not require `DOCKER_BUILDKIT=1` environment variable for starting builds.

Buildx build command supports the features available for `docker build` including the new features in Docker 19.03 such as outputs configuration, inline build caching or specifying target platform. In addition, buildx supports new features not yet available for regular `docker build` like building manifest lists, distributed caching, exporting build results to OCI image tarballs etc.

Buildx is supposed to be flexible and can be run in different configurations that are exposed through a driver concept. Currently, we support a "docker" driver that uses the BuildKit library bundled into the Docker daemon binary, and a "docker-container" driver that automatically launches BuildKit inside a Docker container. We plan to add more drivers in the future, for example, one that would allow running buildx inside an (unprivileged) container.

The user experience of using buildx is very similar across drivers, but there are some features that are not currently supported by the "docker" driver, because the BuildKit library bundled into docker daemon currently uses a different storage component. In contrast, all images built with "docker" driver are automatically added to the "docker images" view by default, whereas when using other drivers the method for outputting an image needs to be selected with `--output`.


## Working with builder instances

By default, buildx will initially use the "docker" driver if it is supported, providing a very similar user experience to the native `docker build`. But using a local shared daemon is only one way to build your applications.

Buildx allows you to create new instances of isolated builders. This can be used for getting a scoped environment for your CI builds that does not change the state of the shared daemon or for isolating the builds for different projects. You can create a new instance for a set of remote nodes, forming a build farm, and quickly switch between them.

New instances can be created with `docker buildx create` command. This will create a new builder instance with a single node based on your current configuration. To use a remote node you can specify the `DOCKER_HOST` or remote context name while creating the new builder. After creating a new instance you can manage its lifecycle with the `inspect`, `stop` and `rm` commands and list all available builders with `ls`. After creating a new builder you can also append new nodes to it.

To switch between different builders, use `docker buildx use <name>`. After running this command the build commands would automatically keep using this builder.

Docker 19.03 also features a new `docker context` command that can be used for giving names for remote Docker API endpoints. Buildx integrates with `docker context` so that all of your contexts automatically get a default builder instance. While creating a new builder instance or when adding a node to it you can also set the context name as the target.

## Building multi-platform images

BuildKit is designed to work well for building for multiple platforms and not only for the architecture and operating system that the user invoking the build happens to run.

When invoking a build, the `--platform` flag can be used to specify the target platform for the build output, (e.g. linux/amd64, linux/arm64, darwin/amd64). When the current builder instance is backed by the "docker-container" driver, multiple platforms can be specified together. In this case, a manifest list will be built, containing images for all of the specified architectures. When this image is used in `docker run` or `docker service`, Docker will pick the correct image based on the node’s platform.

Multi-platform images can be built by mainly three different strategies that are all supported by buildx and Dockerfiles. You can use the QEMU emulation support in the kernel, build on multiple native nodes using the same builder instance or use a stage in Dockerfile to cross-compile to different architectures.

QEMU is the easiest way to get started if your node already supports it (e.g. if you are using Docker Desktop). It requires no changes to your Dockerfile and BuildKit will automatically detect the secondary architectures that are available. When BuildKit needs to run a binary for a different architecture it will automatically load it through a binary registered in the binfmt_misc handler. For QEMU binaries registered with binfmt_misc on the host OS to work transparently inside containers they must be registered with the fix_binary flag. This requires a kernel >= 4.8 and binfmt-support >= 2.1.7. You can check for proper registration by checking if `F` is among the flags in `/proc/sys/fs/binfmt_misc/qemu-*`. While Docker Desktop comes preconfigured with binfmt_misc support for additional platforms, for other installations it likely needs to be installed using [`tonistiigi/binfmt`](https://github.com/tonistiigi/binfmt) image.

```
$ docker run --privileged --rm tonistiigi/binfmt --install all
```

Using multiple native nodes provides better support for more complicated cases not handled by QEMU and generally have better performance. Additional nodes can be added to the builder instance with `--append` flag.

```
# assuming contexts node-amd64 and node-arm64 exist in "docker context ls"
$ docker buildx create --use --name mybuild node-amd64
mybuild
$ docker buildx create --append --name mybuild node-arm64
$ docker buildx build --platform linux/amd64,linux/arm64 .
```

Finally, depending on your project, the language that you use may have good support for cross-compilation. In that case, multi-stage builds in Dockerfiles can be effectively used to build binaries for the platform specified with `--platform` using the native architecture of the build node. List of build arguments like `BUILDPLATFORM` and `TARGETPLATFORM` are available automatically inside your Dockerfile and can be leveraged by the processes running as part of your build.

```
FROM --platform=$BUILDPLATFORM golang:alpine AS build
ARG TARGETPLATFORM
ARG BUILDPLATFORM
RUN echo "I am running on $BUILDPLATFORM, building for $TARGETPLATFORM" > /log
FROM alpine
COPY --from=build /log /log
```


## High-level build options

Buildx also aims to provide support for higher level build concepts that go beyond invoking a single build command. We want to support building all the images in your application together and let the users define project specific reusable build flows that can then be easily invoked by anyone.

BuildKit has great support for efficiently handling multiple concurrent build requests and deduplicating work. While build commands can be combined with general-purpose command runners (eg. make), these tools generally invoke builds in sequence and therefore can’t leverage the full potential of BuildKit parallelization or combine BuildKit’s output for the user. For this use case we have added a command called `docker buildx bake`.

Currently, the bake command supports building images from compose files, similar to `compose build` but allowing all the services to be built concurrently as part of a single request.

There is also support for custom build rules from HCL/JSON files allowing better code reuse and different target groups. The design of bake is in very early stages and we are looking for feedback from users.

# Setting buildx as default builder in Docker 19.03+

Running `docker buildx install` sets up `docker builder` command as an alias to `docker buildx`. This results in the ability to have `docker build` use the current buildx builder.

To remove this alias, you can run `docker buildx uninstall`.


# Contributing

Want to contribute to Buildx? Awesome! You can find information about
contributing to this project in the [CONTRIBUTING.md](/.github/CONTRIBUTING.md)
