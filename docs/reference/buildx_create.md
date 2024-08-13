# buildx create

```text
docker buildx create [OPTIONS] [CONTEXT|ENDPOINT]
```

<!---MARKER_GEN_START-->
Create a new builder instance

### Options

| Name                                      | Type          | Default | Description                                                           |
|:------------------------------------------|:--------------|:--------|:----------------------------------------------------------------------|
| [`--append`](#append)                     | `bool`        |         | Append a node to builder instead of changing it                       |
| `--bootstrap`                             | `bool`        |         | Boot builder after creation                                           |
| [`--buildkitd-config`](#buildkitd-config) | `string`      |         | BuildKit daemon config file                                           |
| [`--buildkitd-flags`](#buildkitd-flags)   | `string`      |         | BuildKit daemon flags                                                 |
| `-D`, `--debug`                           | `bool`        |         | Enable debug logging                                                  |
| [`--driver`](#driver)                     | `string`      |         | Driver to use (available: `docker-container`, `kubernetes`, `remote`) |
| [`--driver-opt`](#driver-opt)             | `stringArray` |         | Options for the driver                                                |
| [`--leave`](#leave)                       | `bool`        |         | Remove a node from builder instead of changing it                     |
| [`--name`](#name)                         | `string`      |         | Builder instance name                                                 |
| [`--node`](#node)                         | `string`      |         | Create/modify node with given name                                    |
| [`--platform`](#platform)                 | `stringArray` |         | Fixed platforms for current node                                      |
| [`--use`](#use)                           | `bool`        |         | Set the current builder instance                                      |


<!---MARKER_GEN_END-->


## Description

Create makes a new builder instance pointing to a Docker context or endpoint,
where context is the name of a context from `docker context ls` and endpoint is
the address for Docker socket (eg. `DOCKER_HOST` value).

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

### <a name="buildkitd-config"></a> Specify a configuration file for the BuildKit daemon (--buildkitd-config)

```text
--buildkitd-config FILE
```

Specifies the configuration file for the BuildKit daemon to use. The
configuration can be overridden by [`--buildkitd-flags`](#buildkitd-flags).
See an [example BuildKit daemon configuration file](https://github.com/moby/buildkit/blob/master/docs/buildkitd.toml.md).

If you don't specify a configuration file, Buildx looks for one by default in:

* `$BUILDX_CONFIG/buildkitd.default.toml`
* `$DOCKER_CONFIG/buildx/buildkitd.default.toml`
* `~/.docker/buildx/buildkitd.default.toml`

Note that if you create a `docker-container` builder and have specified
certificates for registries in the `buildkitd.toml` configuration, the files
will be copied into the container under `/etc/buildkit/certs` and configuration
will be updated to reflect that.

### <a name="buildkitd-flags"></a> Specify options for the BuildKit daemon (--buildkitd-flags)

```text
--buildkitd-flags FLAGS
```

Adds flags when starting the BuildKit daemon. They take precedence over the
configuration file specified by [`--buildkitd-config`](#buildkitd-config). See
`buildkitd --help` for the available flags.

```text
--buildkitd-flags '--debug --debugaddr 0.0.0.0:6666'
```

#### BuildKit daemon network mode

You can specify the network mode for the BuildKit daemon with either the
configuration file specified by [`--buildkitd-config`](#buildkitd-config) using the
`worker.oci.networkMode` option or `--oci-worker-net` flag here. The default
value is `auto` and can be one of `bridge`, `cni`, `host`:

```text
--buildkitd-flags '--oci-worker-net bridge'
```

> [!NOTE]
> Network mode "bridge" is supported since BuildKit v0.13 and will become the
> default in next v0.14.

### <a name="driver"></a> Set the builder driver to use (--driver)

```text
--driver DRIVER
```

Sets the builder driver to be used. A driver is a configuration of a BuildKit
backend. Buildx supports the following drivers:

* `docker` (default)
* `docker-container`
* `kubernetes`
* `remote`

For more information about build drivers, see [here](https://docs.docker.com/build/builders/drivers/).

#### `docker` driver

Uses the builder that is built into the Docker daemon. With this driver,
the [`--load`](buildx_build.md#load) flag is implied by default on
`buildx build`. However, building multi-platform images or exporting cache is
not currently supported.

#### `docker-container` driver

Uses a BuildKit container that will be spawned via Docker. With this driver,
both building multi-platform images and exporting cache are supported.

Unlike `docker` driver, built images will not automatically appear in
`docker images` and [`build --load`](buildx_build.md#load) needs to be used
to achieve that.

#### `kubernetes` driver

Uses Kubernetes pods. With this driver, you can spin up pods with defined
BuildKit container image to build your images.

Unlike `docker` driver, built images will not automatically appear in
`docker images` and [`build --load`](buildx_build.md#load) needs to be used
to achieve that.

#### `remote` driver

Uses a remote instance of BuildKit daemon over an arbitrary connection. With
this driver, you manually create and manage instances of buildkit yourself, and
configure buildx to point at it.

Unlike `docker` driver, built images will not automatically appear in
`docker images` and [`build --load`](buildx_build.md#load) needs to be used
to achieve that.

### <a name="driver-opt"></a> Set additional driver-specific options (--driver-opt)

```text
--driver-opt OPTIONS
```

Passes additional driver-specific options.
For information about available driver options, refer to the detailed
documentation for the specific driver:

* [`docker` driver](https://docs.docker.com/build/builders/drivers/docker/)
* [`docker-container` driver](https://docs.docker.com/build/builders/drivers/docker-container/)
* [`kubernetes` driver](https://docs.docker.com/build/builders/drivers/kubernetes/)
* [`remote` driver](https://docs.docker.com/build/builders/drivers/remote/)

### <a name="leave"></a> Remove a node from a builder (--leave)

The `--leave` flag changes the action of the command to remove a node from a
builder. The builder needs to be specified with `--name` and node that is removed
is set with `--node`.

```console
$ docker buildx create --name mybuilder --node mybuilder0 --leave
```

### <a name="name"></a> Specify the name of the builder (--name)

```text
--name NAME
```

The `--name` flag specifies the name of the builder to be created or modified.
If none is specified, one will be automatically generated.

### <a name="node"></a> Specify the name of the node (--node)

```text
--node NODE
```

The `--node` flag specifies the name of the node to be created or modified. If
you don't specify a name, the node name defaults to the name of the builder it
belongs to, with an index number suffix.

### <a name="platform"></a> Set the platforms supported by the node (--platform)

```text
--platform PLATFORMS
```

The `--platform` flag sets the platforms supported by the node. It expects a
comma-separated list of platforms of the form OS/architecture/variant. The node
will also automatically detect the platforms it supports, but manual values take
priority over the detected ones and can be used when multiple nodes support
building for the same platform.

```console
$ docker buildx create --platform linux/amd64
$ docker buildx create --platform linux/arm64,linux/arm/v7
```

### <a name="use"></a> Automatically switch to the newly created builder (--use)

The `--use` flag automatically switches the current builder to the newly created
one. Equivalent to running `docker buildx use $(docker buildx create ...)`.
