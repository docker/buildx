# buildx inspect

```
docker buildx inspect [NAME]
```

<!---MARKER_GEN_START-->
Inspect current builder instance

### Options

| Name | Description |
| --- | --- |
| [`--bootstrap`](#bootstrap) | Ensure builder has booted before inspecting |
| [`--builder string`](#builder) | Override the configured builder instance |


<!---MARKER_GEN_END-->

## Description

Shows information about the current or specified builder.

## Examples

### <a name="bootstrap"></a> Ensure that the builder is running before inspecting (--bootstrap)

Use the `--bootstrap` option to ensure that the builder is running before
inspecting it. If the driver is `docker-container`, then `--bootstrap` starts
the buildkit container and waits until it is operational. Bootstrapping is
automatically done during build, and therefore not necessary. The same BuildKit
container is used during the lifetime of the associated builder node (as
displayed in `buildx ls`).

### <a name="builder"></a> Override the configured builder instance (--builder)

Same as [`buildx --builder`](buildx.md#builder).

### Get information about a builder instance

By default, `inspect` shows information about the current builder. Specify the
name of the builder to inspect to get information about that builder.
The following example shows information about a builder instance named
`elated_tesla`:

```console
$ docker buildx inspect elated_tesla

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
