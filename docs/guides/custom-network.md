# Using a custom network

[Create a network](https://docs.docker.com/engine/reference/commandline/network_create/)
named `foonet`:

```console
$ docker network create foonet
```

[Create a `docker-container` builder](../reference/buildx_create.md) named
`mybuilder` that will use this network:

```console
$ docker buildx create --use \
  --name mybuilder \
  --driver docker-container \
  --driver-opt "network=foonet"
```

Boot and [inspect `mybuilder`](../reference/buildx_inspect.md):

```console
$ docker buildx inspect --bootstrap
```

[Inspect the builder container](https://docs.docker.com/engine/reference/commandline/inspect/)
and see what network is being used:

```console
$ docker inspect buildx_buildkit_mybuilder0 --format={{.NetworkSettings.Networks}}
map[foonet:0xc00018c0c0]
```

## What's `buildx_buildkit_mybuilder0`?

`buildx_buildkit_mybuilder0` is the container name. It can be broken down like this:

* `buildx_buildkit_` is a hardcoded prefix
* `mybuilder0` is the name of the node (defaults to builder name + position in the list of nodes)

```console
$ docker buildx ls
NAME/NODE     DRIVER/ENDPOINT                        STATUS                 PLATFORMS
mybuilder *   docker-container
  mybuilder0  unix:///var/run/docker.sock            running                linux/amd64, linux/arm64, linux/riscv64, linux/ppc64le, linux/s390x, linux/386, linux/mips64le, linux/mips64, linux/arm/v7, linux/arm/v6     
default       docker
  default     default                                running                linux/amd64, linux/arm64, linux/riscv64, linux/ppc64le, linux/s390x, linux/386, linux/arm/v7, linux/arm/v6
```
