# buildx create

```
docker buildx create [OPTIONS] [CONTEXT|ENDPOINT]
```

<!---MARKER_GEN_START-->
Create a new builder instance

### Options

| Name | Description |
| --- | --- |
| [`--append`](#append) | Append a node to builder instead of changing it |
| `--bootstrap` | Boot builder after creation |
| [`--buildkitd-flags string`](#buildkitd-flags) | Flags for buildkitd daemon |
| [`--config string`](#config) | BuildKit config file |
| [`--driver string`](#driver) | Driver to use (available: `docker`, `docker-container`, `kubernetes`) |
| [`--driver-opt stringArray`](#driver-opt) | Options for the driver |
| [`--leave`](#leave) | Remove a node from builder instead of changing it |
| [`--name string`](#name) | Builder instance name |
| [`--node string`](#node) | Create/modify node with given name |
| [`--platform stringArray`](#platform) | Fixed platforms for current node |
| [`--use`](#use) | Set the current builder instance |


<!---MARKER_GEN_END-->


## Description

Create makes a new builder instance pointing to a docker context or endpoint,
where context is the name of a context from `docker context ls` and endpoint is
the address for docker socket (eg. `DOCKER_HOST` value).

By default, the current Docker configuration is used for determining the
context/endpoint value.

Builder instances are isolated environments where builds can be invoked. All
Docker contexts also get the default builder instance.

## Examples

### <a name="append"></a> Append a new node to an existing builder (--append)

The `--append` flag changes the action of the command to append a new node to an
existing builder specified by `--name`. Buildx will choose an appropriate node
for a build based on the platforms it supports.

```console
$ docker buildx create mycontext1
eager_beaver

$ docker buildx create --name eager_beaver --append mycontext2
eager_beaver
```

### <a name="buildkitd-flags"></a> Specify options for the buildkitd daemon (--buildkitd-flags)

```
--buildkitd-flags FLAGS
```

Adds flags when starting the buildkitd daemon. They take precedence over the
configuration file specified by [`--config`](#config). See `buildkitd --help`
for the available flags.

```
--buildkitd-flags '--debug --debugaddr 0.0.0.0:6666'
```

### <a name="config"></a> Specify a configuration file for the buildkitd daemon (--config)

```
--config FILE
```

Specifies the configuration file for the buildkitd daemon to use. The configuration
can be overridden by [`--buildkitd-flags`](#buildkitd-flags).
See an [example buildkitd configuration file](https://github.com/moby/buildkit/blob/master/docs/buildkitd.toml.md).

Note that if you create a `docker-container` builder and have specified
certificates for registries in the `buildkitd.toml` configuration, the files
will be copied into the container under `/etc/buildkit/certs` and configuration
will be updated to reflect that.

### <a name="driver"></a> Set the builder driver to use (--driver)

```
--driver DRIVER
```

Sets the builder driver to be used. There are two available drivers, each have
their own specificities.

#### `docker` driver

Uses the builder that is built into the docker daemon. With this driver,
the [`--load`](buildx_build.md#load) flag is implied by default on
`buildx build`. However, building multi-platform images or exporting cache is
not currently supported.

#### `docker-container` driver

Uses a BuildKit container that will be spawned via docker. With this driver,
both building multi-platform images and exporting cache are supported.

Unlike `docker` driver, built images will not automatically appear in
`docker images` and [`build --load`](buildx_build.md#load) needs to be used
to achieve that.

#### `kubernetes` driver

Uses a kubernetes pods. With this driver, you can spin up pods with defined
BuildKit container image to build your images.

Unlike `docker` driver, built images will not automatically appear in
`docker images` and [`build --load`](buildx_build.md#load) needs to be used
to achieve that.

### <a name="driver-opt"></a> Set additional driver-specific options (--driver-opt)

```
--driver-opt OPTIONS
```

Passes additional driver-specific options. Details for each driver:

- `docker` - No driver options
- `docker-container`
  - `image=IMAGE` - Sets the container image to be used for running buildkit.
  - `network=NETMODE` - Sets the network mode for running the buildkit container.
  - `cgroup-parent=CGROUP` - Sets the cgroup parent of the buildkit container if docker is using the "cgroupfs" driver. Defaults to `/docker/buildx`.
- `kubernetes`
  - `image=IMAGE` - Sets the container image to be used for running buildkit.
  - `namespace=NS` - Sets the Kubernetes namespace. Defaults to the current namespace.
  - `replicas=N` - Sets the number of `Pod` replicas. Defaults to 1.
  - `requests.cpu` - Sets the request CPU value specified in units of Kubernetes CPU. Example `requests.cpu=100m`, `requests.cpu=2`
  - `requests.memory` - Sets the request memory value specified in bytes or with a valid suffix. Example `requests.memory=500Mi`, `requests.memory=4G`
  - `limits.cpu` - Sets the limit CPU value specified in units of Kubernetes CPU. Example `limits.cpu=100m`, `limits.cpu=2`
  - `limits.memory` - Sets the limit memory value specified in bytes or with a valid suffix. Example `limits.memory=500Mi`, `limits.memory=4G`
  - `nodeselector="label1=value1,label2=value2"` - Sets the kv of `Pod` nodeSelector. No Defaults. Example `nodeselector=kubernetes.io/arch=arm64`
  - `rootless=(true|false)` - Run the container as a non-root user without `securityContext.privileged`. [Using Ubuntu host kernel is recommended](https://github.com/moby/buildkit/blob/master/docs/rootless.md). Defaults to false.
  - `loadbalance=(sticky|random)` - Load-balancing strategy. If set to "sticky", the pod is chosen using the hash of the context path. Defaults to "sticky"
  - `qemu.install=(true|false)` - Install QEMU emulation for multi platforms support.
  - `qemu.image=IMAGE` - Sets the QEMU emulation image. Defaults to `tonistiigi/binfmt:latest`

### <a name="leave"></a> Remove a node from a builder (--leave)

The `--leave` flag changes the action of the command to remove a node from a
builder. The builder needs to be specified with `--name` and node that is removed
is set with `--node`.

```console
$ docker buildx create --name mybuilder --node mybuilder0 --leave
```

### <a name="name"></a> Specify the name of the builder (--name)

```
--name NAME
```

The `--name` flag specifies the name of the builder to be created or modified.
If none is specified, one will be automatically generated.

### <a name="node"></a> Specify the name of the node (--node)

```
--node NODE
```

The `--node` flag specifies the name of the node to be created or modified. If
none is specified, it is the name of the builder it belongs to, with an index
number suffix.

### <a name="platform"></a> Set the platforms supported by the node (--platform)

```
--platform PLATFORMS
```

The `--platform` flag sets the platforms supported by the node. It expects a
comma-separated list of platforms of the form OS/architecture/variant. The node
will also automatically detect the platforms it supports, but manual values take
priority over the detected ones and can be used when multiple nodes support
building for the same platform.

```console
$ docker buildx create --platform linux/amd64
$ docker buildx create --platform linux/arm64,linux/arm/v8
```

### <a name="use"></a> Automatically switch to the newly created builder (--use)

The `--use` flag automatically switches the current builder to the newly created
one. Equivalent to running `docker buildx use $(docker buildx create ...)`.
