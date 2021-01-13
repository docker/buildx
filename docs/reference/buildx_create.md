# buildx create

```
Usage:  docker buildx create [OPTIONS] [CONTEXT|ENDPOINT]

Create a new builder instance

Options:
      --append                   Append a node to builder instead of changing it
      --builder string           Override the configured builder instance
      --buildkitd-flags string   Flags for buildkitd daemon
      --config string            BuildKit config file
      --driver string            Driver to use (available: [docker docker-container kubernetes])
      --driver-opt stringArray   Options for the driver
      --leave                    Remove a node from builder instead of changing it
      --name string              Builder instance name
      --node string              Create/modify node with given name
      --platform stringArray     Fixed platforms for current node
      --use                      Set the current builder instance
```

## Description

Create makes a new builder instance pointing to a docker context or endpoint,
where context is the name of a context from `docker context ls` and endpoint is
the address for docker socket (eg. `DOCKER_HOST` value).

By default, the current docker configuration is used for determining the
context/endpoint value.

Builder instances are isolated environments where builds can be invoked. All
docker contexts also get the default builder instance.

### `--append`

Changes the action of the command to appends a new node to an existing builder
specified by `--name`. Buildx will choose an appropriate node for a build based
on the platforms it supports.

Example:

```console
$ docker buildx create mycontext1
eager_beaver

$ docker buildx create --name eager_beaver --append mycontext2
eager_beaver
```

### `--buildkitd-flags FLAGS`

Adds flags when starting the buildkitd daemon. They take precedence over the
configuration file specified by [`--config`](#--config-file). See `buildkitd --help`
for the available flags.

Example:

```console
--buildkitd-flags '--debug --debugaddr 0.0.0.0:6666'
```

### `--config FILE`

Specifies the configuration file for the buildkitd daemon to use. The configuration
can be overridden by [`--buildkitd-flags`](#--buildkitd-flags-flags).
See an [example buildkitd configuration file](https://github.com/moby/buildkit/blob/master/docs/buildkitd.toml.md).

### `--driver DRIVER`

Sets the builder driver to be used. There are two available drivers, each have
their own specificities.

- `docker` - Uses the builder that is built into the docker daemon. With this
  driver, the [`--load`](buildx_build.md#--load) flag is implied by default on
  `buildx build`. However, building multi-platform images or exporting cache is
  not currently supported.
- `docker-container` - Uses a buildkit container that will be spawned via docker.
  With this driver, both building multi-platform images and exporting cache are
  supported. However, images built will not automatically appear in `docker images`
  (see [`build --load`](buildx_build.md#--load)).
- `kubernetes` - Uses a kubernetes pods. With this driver, you can spin up pods
  with defined buildkit container image to build your images.


### `--driver-opt OPTIONS`

Passes additional driver-specific options. Details for each driver:

- `docker` - No driver options
- `docker-container`
    - `image=IMAGE` - Sets the container image to be used for running buildkit.
    - `network=NETMODE` - Sets the network mode for running the buildkit container.
    - Example:

      ```console
      --driver docker-container --driver-opt image=moby/buildkit:master,network=host
      ```
- `kubernetes`
    - `image=IMAGE` - Sets the container image to be used for running buildkit.
    - `namespace=NS` - Sets the Kubernetes namespace. Defaults to the current namespace.
    - `replicas=N` - Sets the number of `Pod` replicas. Defaults to 1.
    - `nodeselector="label1=value1,label2=value2"` - Sets the kv of `Pod` nodeSelector. No Defaults. Example `nodeselector=kubernetes.io/arch=arm64`
    - `rootless=(true|false)` - Run the container as a non-root user without `securityContext.privileged`. [Using Ubuntu host kernel is recommended](https://github.com/moby/buildkit/blob/master/docs/rootless.md). Defaults to false.
    - `loadbalance=(sticky|random)` - Load-balancing strategy. If set to "sticky", the pod is chosen using the hash of the context path. Defaults to "sticky"

### `--leave`

Changes the action of the command to removes a node from a builder. The builder
needs to be specified with `--name` and node that is removed is set with `--node`.

Example:

```console
docker buildx create --name mybuilder --node mybuilder0 --leave
```

### `--name NAME`

Specifies the name of the builder to be created or modified. If none is specified,
one will be automatically generated.

### `--node NODE`

Specifies the name of the node to be created or modified. If none is specified,
it is the name of the builder it belongs to, with an index number suffix.

### `--platform PLATFORMS`

Sets the platforms supported by the node. It expects a comma-separated list of
platforms of the form OS/architecture/variant. The node will also automatically
detect the platforms it supports, but manual values take priority over the
detected ones and can be used when multiple nodes support building for the same
platform.

Example:

```console
docker buildx create --platform linux/amd64
docker buildx create --platform linux/arm64,linux/arm/v8
```

### `--use`

Automatically switches the current builder to the newly created one. Equivalent
to running `docker buildx use $(docker buildx create ...)`.
