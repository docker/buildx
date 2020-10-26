# buildx
### Docker CLI plugin for extended build capabilities with BuildKit

_buildx is Tech Preview_

### TL;DR

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
    + [`buildx build [OPTIONS] PATH | URL | -`](#buildx-build-options-path--url---)
    + [`buildx create [OPTIONS] [CONTEXT|ENDPOINT]`](#buildx-create-options-contextendpoint)
    + [`buildx use NAME`](#buildx-use-name)
    + [`buildx inspect [NAME]`](#buildx-inspect-name)
    + [`buildx ls`](#buildx-ls)
    + [`buildx stop [NAME]`](#buildx-stop-name)
    + [`buildx rm [NAME]`](#buildx-rm-name)
    + [`buildx bake [OPTIONS] [TARGET...]`](#buildx-bake-options-target)
    + [`buildx imagetools create [OPTIONS] [SOURCE] [SOURCE...]`](#buildx-imagetools-create-options-source-source)
    + [`buildx imagetools inspect NAME`](#buildx-imagetools-inspect-name)
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

Buildx is supposed to be flexible and can be run in different configurations that are exposed through a driver concept. Currently, we support a "docker" driver that uses the BuildKit library bundled into the docker daemon binary, and a "docker-container" driver that automatically launches BuildKit inside a Docker container. We plan to add more drivers in the future, for example, one that would allow running buildx inside an (unprivileged) container.

The user experience of using buildx is very similar across drivers, but there are some features that are not currently supported by the "docker" driver, because the BuildKit library bundled into docker daemon currently uses a different storage component. In contrast, all images built with "docker" driver are automatically added to the "docker images" view by default, whereas when using other drivers the method for outputting an image needs to be selected with `--output`.


## Working with builder instances

By default, buildx will initially use the "docker" driver if it is supported, providing a very similar user experience to the native `docker build`. But using a local shared daemon is only one way to build your applications.

Buildx allows you to create new instances of isolated builders. This can be used for getting a scoped environment for your CI builds that does not change the state of the shared daemon or for isolating the builds for different projects. You can create a new instance for a set of remote nodes, forming a build farm, and quickly switch between them.

New instances can be created with `docker buildx create` command. This will create a new builder instance with a single node based on your current configuration. To use a remote node you can specify the `DOCKER_HOST` or remote context name while creating the new builder. After creating a new instance you can manage its lifecycle with the `inspect`, `stop` and `rm` commands and list all available builders with `ls`. After creating a new builder you can also append new nodes to it.

To switch between different builders use `docker buildx use <name>`. After running this command the build commands would automatically keep using this builder.

Docker 19.03 also features a new `docker context` command that can be used for giving names for remote Docker API endpoints. Buildx integrates with `docker context` so that all of your contexts automatically get a default builder instance. While creating a new builder instance or when adding a node to it you can also set the context name as the target.

## Building multi-platform images

BuildKit is designed to work well for building for multiple platforms and not only for the architecture and operating system that the user invoking the build happens to run.

When invoking a build, the `--platform` flag can be used to specify the target platform for the build output, (e.g. linux/amd64, linux/arm64, darwin/amd64). When the current builder instance is backed by the "docker-container" driver, multiple platforms can be specified together. In this case, a manifest list will be built, containing images for all of the specified architectures. When this image is used in `docker run` or `docker service`, Docker will pick the correct image based on the node’s platform.

Multi-platform images can be built by mainly three different strategies that are all supported by buildx and Dockerfiles. You can use the QEMU emulation support in the kernel, build on multiple native nodes using the same builder instance or use a stage in Dockerfile to cross-compile to different architectures.

QEMU is the easiest way to get started if your node already supports it (e.g. if you are using Docker Desktop). It requires no changes to your Dockerfile and BuildKit will automatically detect the secondary architectures that are available. When BuildKit needs to run a binary for a different architecture it will automatically load it through a binary registered in the binfmt_misc handler. For QEMU binaries registered with binfmt_misc on the host OS to work transparently inside containers they must be registed with the fix_binary flag. This requires a kernel >= 4.8 and binfmt-support >= 2.1.7. You can check for proper registration by checking if `F` is among the flags in `/proc/sys/fs/binfmt_misc/qemu-*`. While Docker Desktop comes preconfigured with binfmt_misc support for additional platforms, for other installations it likely needs to be installed using [`tonistiigi/binfmt`](https://github.com/tonistiigi/binfmt) image.

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



# Documentation

### `buildx build [OPTIONS] PATH | URL | -`

The `buildx build` command starts a build using BuildKit. This command is similar to the UI of `docker build` command and takes the same flags and arguments.

Options:

| Flag | Description |
| --- | --- |
| --add-host []         | Add a custom host-to-IP mapping (host:ip)
| --allow []        | Allow extra privileged entitlement, e.g. network.host, security.insecure
| --build-arg []    | Set build-time variables
| --cache-from []   | External cache sources (eg. user/app:cache, type=local,src=path/to/dir)
| --cache-to []     | Cache export destinations (eg. user/app:cache, type=local,dest=path/to/dir)
| --file string              | Name of the Dockerfile (Default is 'PATH/Dockerfile')
| --iidfile string           | Write the image ID to the file
| --label []        | Set metadata for an image
| --load                     | Shorthand for --output=type=docker
| --network string           | Set the networking mode for the RUN instructions during build (default "default")
| --no-cache                 | Do not use cache when building the image
| --output []       | Output destination (format: type=local,dest=path)
| --platform []     | Set target platform for build
| --progress string          | Set type of progress output (auto, plain, tty). Use plain to show container output (default "auto")
| --pull                     | Always attempt to pull a newer version of the image
| --push                     | Shorthand for --output=type=registry
| --secret []       | Secret file to expose to the build: id=mysecret,src=/local/secret
| --ssh []          | SSH agent socket or keys to expose to the build (format: default\|&#60;id&#62;[=&#60;socket&#62;\|&#60;key&#62;[,&#60;key&#62;]])
| --tag []          | Name and optionally a tag in the 'name:tag' format
| --target string            | Set the target build stage to build.

For documentation on most of these flags refer to `docker build` documentation in https://docs.docker.com/engine/reference/commandline/build/ . In here we’ll document a subset of the new flags.

####  ` --platform=value[,value]`

Set the target platform for the build. All `FROM` commands inside the Dockerfile without their own `--platform` flag will pull base images for this platform and this value will also be the platform of the resulting image. The default value will be the current platform of the buildkit daemon. 

When using `docker-container` driver with `buildx`, this flag can accept multiple values as an input separated by a comma. With multiple values the result will be built for all of the specified platforms and joined together into a single manifest list.

If the`Dockerfile` needs to invoke the `RUN` command, the builder needs runtime support for the specified platform. In a clean setup, you can only execute `RUN` commands for your system architecture. If your kernel supports binfmt_misc https://en.wikipedia.org/wiki/Binfmt_misc   launchers for secondary architectures buildx will pick them up automatically. Docker desktop releases come with binfmt_misc automatically configured for `arm64` and `arm` architectures. You can see what runtime platforms your current builder instance supports by running `docker buildx inspect --bootstrap`.

Inside a `Dockerfile`, you can access the current platform value through `TARGETPLATFORM` build argument. Please refer to `docker build` documentation for the full description of automatic platform argument variants https://docs.docker.com/engine/reference/builder/#automatic-platform-args-in-the-global-scope .

The formatting for the platform specifier is defined in https://github.com/containerd/containerd/blob/v1.2.6/platforms/platforms.go#L63  .

Examples:
```
docker buildx build --platform=linux/arm64 .
docker buildx build --platform=linux/amd64,linux/arm64,linux/arm/v7 .
docker buildx build --platform=darwin .
```

#### `-o, --output=[PATH,-,type=TYPE[,KEY=VALUE]`

Sets the export action for the build result. In `docker build` all builds finish by creating a container image and exporting it to `docker images`. `buildx` makes this step configurable allowing results to be exported directly to the client, oci image tarballs, registry etc.

Buildx with `docker` driver currently only supports local, tarball exporter and image exporter. `docker-container` driver supports all the exporters.

If just the path is specified as a value, `buildx` will use the local exporter with this path as the destination. If the value is “-”, `buildx` will use `tar` exporter and write to `stdout`.

Examples:

```
docker buildx build -o . .
docker buildx build -o outdir .
docker buildx build -o - - > out.tar
docker buildx build -o type=docker .
docker buildx build -o type=docker,dest=- . > myimage.tar
docker buildx build -t tonistiigi/foo -o type=registry 
````

Supported exported types are:

##### `local`

The `local` export type writes all result files to a directory on the client. The new files will be owned by the current user. On multi-platform builds, all results will be put in subdirectories by their platform.

Attribute key:

- `dest` - destination directory where files will be written

##### `tar`

The `tar` export type writes all result files as a single tarball on the client. On multi-platform builds all results will be put in subdirectories by their platform.

Attribute key:

- `dest` - destination path where tarball will be written. “-” writes to stdout.

##### `oci`

The `oci` export type writes the result image or manifest list as an OCI image layout tarball https://github.com/opencontainers/image-spec/blob/master/image-layout.md on the client.

Attribute key:

- `dest` - destination path where tarball will be written. “-” writes to stdout.

##### `docker`

The `docker` export type writes the single-platform result image as a Docker image specification tarball https://github.com/moby/moby/blob/master/image/spec/v1.2.md on the client. Tarballs created by this exporter are also OCI compatible.

Currently, multi-platform images cannot be exported with the `docker` export type. The most common usecase for multi-platform images is to directly push to a registry (see [`registry`](#registry)).

Attribute keys:

- `dest` - destination path where tarball will be written. If not specified the tar will be loaded automatically to the current docker instance.
- `context` - name for the docker context where to import the result

##### `image`

The `image` exporter writes the build result as an image or a manifest list. When using `docker` driver the image will appear in `docker images`. Optionally image can be automatically pushed to a registry by specifying attributes.

Attribute keys:

- `name` - name (references) for the new image.
- `push` - boolean to automatically push the image.

##### `registry` 

The `registry` exporter is a shortcut for `type=image,push=true`.


#### `--push`

Shorthand for [`--output=type=registry`](#registry). Will automatically push the build result to registry.

#### `--load`

Shorthand for [`--output=type=docker`](#docker). Will automatically load the single-platform build result to `docker images`.

#### `--cache-from=[NAME|type=TYPE[,KEY=VALUE]]`

Use an external cache source for a build. Supported types are `registry` and `local`. The `registry` source can import cache from a cache manifest or (special) image configuration on the registry. The `local` source can import cache from local files previously exported with `--cache-to`.

If no type is specified, `registry` exporter is used with a specified reference.

`docker` driver currently only supports importing build cache from the registry.

Examples:
```
docker buildx build --cache-from=user/app:cache .
docker buildx build --cache-from=user/app .
docker buildx build --cache-from=type=registry,ref=user/app .
docker buildx build --cache-from=type=local,src=path/to/cache .
```

#### `--cache-to=[NAME|type=TYPE[,KEY=VALUE]]`

Export build cache to an external cache destination. Supported types are `registry`, `local` and `inline`. Registry exports build cache to a cache manifest in the registry, local exports cache to a local directory on the client and inline writes the cache metadata into the image configuration.

`docker` driver currently only supports exporting inline cache metadata to image configuration. Alternatively, `--build-arg BUILDKIT_INLINE_CACHE=1` can be used to trigger inline cache exporter.

Attribute key:

- `mode` - Specifies how many layers are exported with the cache. “min” on only exports layers already in the final build build stage, “max” exports layers for all stages. Metadata is always exported for the whole build.

Examples:
```
docker buildx build --cache-to=user/app:cache .
docker buildx build --cache-to=type=inline .
docker buildx build --cache-to=type=registry,ref=user/app .
docker buildx build --cache-to=type=local,dest=path/to/cache .
```

#### `--allow=ENTITLEMENT`

Allow extra privileged entitlement. List of entitlements:

- `network.host` - Allows executions with host networking.
- `security.insecure` - Allows executions without sandbox. See [related Dockerfile extensions](https://github.com/moby/buildkit/blob/master/frontend/dockerfile/docs/experimental.md#run---securityinsecuresandbox).

For entitlements to be enabled, the `buildkitd` daemon also needs to allow them with `--allow-insecure-entitlement` (see [`create --buildkitd-flags`](#--buildkitd-flags-flags))

Example:
```
$ docker buildx create --use --name insecure-builder --buildkitd-flags '--allow-insecure-entitlement security.insecure'
$ docker buildx build --allow security.insecure .
```

### `buildx create [OPTIONS] [CONTEXT|ENDPOINT]`

Create makes a new builder instance pointing to a docker context or endpoint, where context is the name of a context from `docker context ls` and endpoint is the address for docker socket (eg. `DOCKER_HOST` value).

By default, the current docker configuration is used for determining the context/endpoint value.

Builder instances are isolated environments where builds can be invoked. All docker contexts also get the default builder instance.

Options:

| Flag | Description |
| --- | --- |
| --append                 | Append a node to builder instead of changing it
| --buildkitd-flags string | Flags for buildkitd daemon
| --config string          | BuildKit config file
| --driver string          | Driver to use (eg. docker-container)
| --driver-opt stringArray | Options for the driver
| --leave                  | Remove a node from builder instead of changing it
| --name string            | Builder instance name
| --node string            | Create/modify node with given name
| --platform stringArray   | Fixed platforms for current node
| --use                    | Set the current builder instance

#### `--append`

Changes the action of the command to appends a new node to an existing builder specified by `--name`. Buildx will choose an appropriate node for a build based on the platforms it supports.

Example:
```
$ docker buildx create mycontext1
eager_beaver
$ docker buildx create --name eager_beaver --append mycontext2
eager_beaver
```

#### `--buildkitd-flags FLAGS`

Adds flags when starting the buildkitd daemon. They take precedence over the configuration file specified by [`--config`](#--config-file). See `buildkitd --help` for the available flags.

Example:
```
--buildkitd-flags '--debug --debugaddr 0.0.0.0:6666'
```

#### `--config FILE`

Specifies the configuration file for the buildkitd daemon to use. The configuration can be overridden by [`--buildkitd-flags`](#--buildkitd-flags-flags). See an [example buildkitd configuration file](https://github.com/moby/buildkit/blob/master/docs/buildkitd.toml.md).

#### `--driver DRIVER`

Sets the builder driver to be used. There are two available drivers, each have their own specificities.

- `docker` - Uses the builder that is built into the docker daemon. With this driver, the [`--load`](#--load) flag is implied by default on `buildx build`. However, building multi-platform images or exporting cache is not currently supported.

- `docker-container` - Uses a buildkit container that will be spawned via docker. With this driver, both building multi-platform images and exporting cache are supported. However, images built will not automatically appear in `docker images` (see [`build --load`](#--load)).

- `kubernetes` - Uses a kubernetes pods. With this driver , you can spin up pods with defined buildkit container image to build your images. 


#### `--driver-opt OPTIONS`

Passes additional driver-specific options. Details for each driver:

- `docker` - No driver options
- `docker-container`
  - `image=IMAGE` - Sets the container image to be used for running buildkit.
  - `network=NETMODE` - Sets the network mode for running the buildkit container.
  - Example:
    ```
    --driver docker-container --driver-opt image=moby/buildkit:master,network=host
    ```
- `kubernetes`
  - `image=IMAGE` - Sets the container image to be used for running buildkit.
  - `namespace=NS` - Sets the Kubernetes namespace. Defaults to the current namespace.
  - `replicas=N` - Sets the number of `Pod` replicas. Defaults to 1.
  - `nodeselector="label1=value1,label2=value2"` - Sets the kv of `Pod` nodeSelector. No Defaults. Example `nodeselector=kubernetes.io/arch=arm64`
  - `rootless=(true|false)` - Run the container as a non-root user without `securityContext.privileged`. [Using Ubuntu host kernel is recommended](https://github.com/moby/buildkit/blob/master/docs/rootless.md). Defaults to false.
  - `loadbalance=(sticky|random)` - Load-balancing strategy. If set to "sticky", the pod is chosen using the hash of the context path. Defaults to "sticky"

#### `--leave`

Changes the action of the command to removes a node from a builder. The builder needs to be specified with `--name` and node that is removed is set with `--node`.

Example:
```
docker buildx create --name mybuilder --node mybuilder0 --leave
```

#### `--name NAME`

Specifies the name of the builder to be created or modified. If none is specified, one will be automatically generated.

#### `--node NODE`

Specifies the name of the node to be created or modified. If none is specified, it is the name of the builder it belongs to, with an index number suffix.

#### `--platform PLATFORMS`

Sets the platforms supported by the node. It expects a comma-separated list of platforms of the form OS/architecture/variant. The node will also automatically detect the platforms it supports, but manual values take priority over the detected ones and can be used when multiple nodes support building for the same platform.

Example:
```
docker buildx create --platform linux/amd64
docker buildx create --platform linux/arm64,linux/arm/v8
```

#### `--use`

Automatically switches the current builder to the newly created one. Equivalent to running `docker buildx use $(docker buildx create ...)`.

### `buildx use NAME`

Switches the current builder instance. Build commands invoked after this command will run on a specified builder. Alternatively, a context name can be used to switch to the default builder of that context.

### `buildx inspect [NAME]`

Shows information about the current or specified builder.

Example:
```
Name:   elated_tesla
Driver: docker-container

Nodes:
Name:      elated_tesla0
Endpoint:  unix:///var/run/docker.sock
Status:    running
Platforms: linux/amd64

Name:      elated_tesla1
Endpoint:  ssh://ubuntu@1.2.3.4
Status:    running
Platforms: linux/arm64, linux/arm/v7, linux/arm/v6
```

#### `--bootstrap`

Ensures that the builder is running before inspecting it. If the driver is `docker-container`, then `--bootstrap` starts the buildkit container and waits until it is operational. Bootstrapping is automatically done during build, it is thus not necessary. The same BuildKit container is used during the lifetime of the associated builder node (as displayed in `buildx ls`).

### `buildx ls`

Lists all builder instances and the nodes for each instance

Example:

```
docker buildx ls
NAME/NODE       DRIVER/ENDPOINT             STATUS  PLATFORMS
elated_tesla *  docker-container
  elated_tesla0 unix:///var/run/docker.sock running linux/amd64
  elated_tesla1 ssh://ubuntu@1.2.3.4        running linux/arm64, linux/arm/v7, linux/arm/v6
default         docker
  default       default                     running linux/amd64
```

Each builder has one or more nodes associated with it. The current builder’s name is marked with a `*`.

### `buildx stop [NAME]`

Stops the specified or current builder. This will not prevent buildx build to restart the builder. The implementation of stop depends on the driver.

### `buildx rm [NAME]`

Removes the specified or current builder. It is a no-op attempting to remove the default builder.

### `buildx bake [OPTIONS] [TARGET...]`

Bake is a high-level build command.

Each specified target will run in parallel as part of the build.

Options:

| Flag | Description |
| --- | --- |
|  -f, --file stringArray  | Build definition file
|      --load              | Shorthand for --set=*.output=type=docker
|      --no-cache          | Do not use cache when building the image
|      --print             | Print the options without building
|      --progress string   | Set type of progress output (auto, plain, tty). Use plain to show container output (default "auto")
|      --pull              | Always attempt to pull a newer version of the image
|      --push              | Shorthand for --set=*.output=type=registry
|      --set stringArray   | Override target value (eg: targetpattern.key=value)

#### `-f, --file FILE`

Specifies the bake definition file. The file can be a Docker Compose, JSON or HCL file. If multiple files are specified they are all read and configurations are combined. By default, if no files are specified, the following are parsed:
docker-compose.yml
docker-compose.yaml
docker-bake.json
docker-bake.override.json
docker-bake.hcl
docker-bake.override.hcl

#### `--no-cache`

Same as `build --no-cache`. Do not use cache when building the image.

#### `--print`

Prints the resulting options of the targets desired to be built, in a JSON format, without starting a build.

```
$ docker buildx bake -f docker-bake.hcl --print db
{
   "target": {
      "db": {
         "context": "./",
         "dockerfile": "Dockerfile",
         "tags": [
            "docker.io/tiborvass/db"
         ]
      }
   }
}
```

#### `--progress`

Same as `build --progress`. Set type of progress output (auto, plain, tty). Use plain to show container output (default "auto").

#### `--pull`

Same as `build --pull`.

#### `--set targetpattern.key[.subkey]=value`

Override target configurations from command line. The pattern matching syntax is defined in https://golang.org/pkg/path/#Match.

Example:
```
docker buildx bake --set target.args.mybuildarg=value
docker buildx bake --set target.platform=linux/arm64
docker buildx bake --set foo*.args.mybuildarg=value	# overrides build arg for all targets starting with 'foo'
docker buildx bake --set *.platform=linux/arm64		# overrides platform for all targets
docker buildx bake --set foo*.no-cache                  # bypass caching only for targets starting with 'foo'
```

Complete list of overridable fields:
	args, cache-from, cache-to, context, dockerfile, labels, no-cache, output, platform, pull, secrets, ssh, tags, target

#### File definition

In addition to compose files, bake supports a JSON and an equivalent HCL file format for defining build groups and targets.

A target reflects a single docker build invocation with the same options that you would specify for `docker build`. A group is a grouping of targets.

Multiple files can include the same target and final build options will be determined by merging them together. 

In the case of compose files, each service corresponds to a target.

A group can specify its list of targets with the `targets` option. A target can inherit build options by setting the `inherits` option to the list of targets or groups to inherit from.

Note: Design of bake command is work in progress, the user experience may change based on feedback.



Example HCL defintion:

```
group "default" {
	targets = ["db", "webapp-dev"]
}

target "webapp-dev" {
	dockerfile = "Dockerfile.webapp"
	tags = ["docker.io/username/webapp"]
}

target "webapp-release" {
	inherits = ["webapp-dev"]
	platforms = ["linux/amd64", "linux/arm64"]
}

target "db" {
	dockerfile = "Dockerfile.db"
	tags = ["docker.io/username/db"]
}
```

Complete list of valid target fields:
	args, cache-from, cache-to, context, dockerfile, inherits, labels, no-cache, output, platform, pull, secrets, ssh, tags, target

#### HCL variables and functions

Similar to how Terraform provides a way to [define variables](https://www.terraform.io/docs/configuration/variables.html#declaring-an-input-variable), the HCL file format also supports variable block definitions. These can be used to define variables with values provided by the current environment or a default value when unset.



Example of using interpolation to tag an image with the git sha:

```
$ cat <<'EOF' > docker-bake.hcl
variable "TAG" {
	default = "latest"
}

group "default" {
	targets = ["webapp"]
}

target "webapp" {
	tags = ["docker.io/username/webapp:${TAG}"]
}
EOF

$ docker buildx bake --print webapp
{
   "target": {
      "webapp": {
         "context": ".",
         "dockerfile": "Dockerfile",
         "tags": [
            "docker.io/username/webapp:latest"
         ]
      }
   }
}

$ TAG=$(git rev-parse --short HEAD) docker buildx bake --print webapp
{
   "target": {
      "webapp": {
         "context": ".",
         "dockerfile": "Dockerfile",
         "tags": [
            "docker.io/username/webapp:985e9e9"
         ]
      }
   }
}
```


A [set of generally useful functions](https://github.com/docker/buildx/blob/master/bake/hcl.go#L19-L65) provided by [go-cty](https://github.com/zclconf/go-cty/tree/master/cty/function/stdlib) are avaialble for use in HCL files. In addition, [user defined functions](https://github.com/hashicorp/hcl/tree/hcl2/ext/userfunc) are also supported.



Example of using the `add` function:

```
$ cat <<'EOF' > docker-bake.hcl
variable "TAG" {
	default = "latest"
}

group "default" {
	targets = ["webapp"]
}

target "webapp" {
	args = {
		buildno = "${add(123, 1)}"
	}
}
EOF

$ docker buildx bake --print webapp
{
   "target": {
      "webapp": {
         "context": ".",
         "dockerfile": "Dockerfile",
         "args": {
            "buildno": "124"
         }
      }
   }
}
```

Example of defining an `increment` function:

```
$ cat <<'EOF' > docker-bake.hcl
function "increment" {
	params = [number]
	result = number + 1
}

group "default" {
	targets = ["webapp"]
}

target "webapp" {
	args = {
		buildno = "${increment(123)}"
	}
}
EOF

$ docker buildx bake --print webapp
{
   "target": {
      "webapp": {
         "context": ".",
         "dockerfile": "Dockerfile",
         "args": {
            "buildno": "124"
         }
      }
   }
}
```

Example of only adding tags if a variable is not empty using an `notequal` function:

```
$ cat <<'EOF' > docker-bake.hcl
variable "TAG" {default="" }

group "default" {
	targets = [
    "webapp",
  ]
}

target "webapp" {
  context="."
  dockerfile="Dockerfile"
  tags = [
      "my-image:latest",
      notequal("",TAG) ? "my-image:${TAG}": "",
  ]
}
EOF

$ docker buildx bake --print webapp
{
   "target": {
      "webapp": {
         "context": ".",
         "dockerfile": "Dockerfile",
         "tags": [
            "my-image:latest"
         ]
      }
   }
}
```


### `buildx imagetools create [OPTIONS] [SOURCE] [SOURCE...]`

Imagetools contains commands for working with manifest lists in the registry. These commands are useful for inspecting multi-platform build results.

Create creates a new manifest list based on source manifests. The source manifests can be manifest lists or single platform distribution manifests and must already exist in the registry where the new manifest is created. If only one source is specified create performs a carbon copy.

Options:

| Flag | Description |
| --- | --- |
|      --append            | Append to existing manifest
|      --dry-run           | Show final image instead of pushing
|  -f, --file stringArray  | Read source descriptor from file
|  -t, --tag stringArray   | Set reference for new image

#### `--append`

Append appends the new sources to an existing manifest list in the destination.

#### `--dry-run`

Do not push the image, just show it.

#### `-f, --file FILE`

Reads source from files. A source can be a manifest digest, manifest reference or a JSON of OCI descriptor object.

#### `-t, --tag IMAGE`

Name of the image to be created.

Examples:

```
docker buildx imagetools create --dry-run alpine@sha256:5c40b3c27b9f13c873fefb2139765c56ce97fd50230f1f2d5c91e55dec171907 sha256:c4ba6347b0e4258ce6a6de2401619316f982b7bcc529f73d2a410d0097730204

docker buildx imagetools create -t tonistiigi/myapp -f image1 -f image2 
```


### `buildx imagetools inspect NAME`

Show details of image in the registry.

Example:
```
$ docker buildx imagetools inspect alpine
Name:      docker.io/library/alpine:latest
MediaType: application/vnd.docker.distribution.manifest.list.v2+json
Digest:    sha256:28ef97b8686a0b5399129e9b763d5b7e5ff03576aa5580d6f4182a49c5fe1913

Manifests:
  Name:      docker.io/library/alpine:latest@sha256:5c40b3c27b9f13c873fefb2139765c56ce97fd50230f1f2d5c91e55dec171907
  MediaType: application/vnd.docker.distribution.manifest.v2+json
  Platform:  linux/amd64

  Name:      docker.io/library/alpine:latest@sha256:c4ba6347b0e4258ce6a6de2401619316f982b7bcc529f73d2a410d0097730204
  MediaType: application/vnd.docker.distribution.manifest.v2+json
  Platform:  linux/arm/v6

 ...
```

#### `--raw`

Raw prints the original JSON bytes instead of the formatted output.


# Setting buildx as default builder in Docker 19.03+

Running `docker buildx install` sets up `docker builder` command as an alias to `docker buildx`. This results in the ability to have `docker build` use the current buildx builder.

To remove this alias, you can run `docker buildx uninstall`.


# Contributing

Want to contribute to Buildx? Awesome! You can find information about
contributing to this project in the [CONTRIBUTING.md](/.github/CONTRIBUTING.md)
