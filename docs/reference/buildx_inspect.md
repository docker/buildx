# buildx inspect

```text
docker buildx inspect [NAME]
```

<!---MARKER_GEN_START-->
Inspect current builder instance

### Options

| Name                        | Type     | Default | Description                                 |
|:----------------------------|:---------|:--------|:--------------------------------------------|
| [`--bootstrap`](#bootstrap) | `bool`   |         | Ensure builder has booted before inspecting |
| [`--builder`](#builder)     | `string` |         | Override the configured builder instance    |
| `-D`, `--debug`             | `bool`   |         | Enable debug logging                        |


<!---MARKER_GEN_END-->

## Description

Shows information about the current or specified builder.

## Examples

### <a name="bootstrap"></a> Ensure that the builder is running before inspecting (--bootstrap)

Use the `--bootstrap` option to ensure that the builder is running before
inspecting it. If the driver is `docker-container`, then `--bootstrap` starts
the BuildKit container and waits until it's operational. Bootstrapping is
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

> [!NOTE]
> The asterisk (`*`) next to node build platform(s) indicate they have been
> manually set during `buildx create`. Otherwise the platforms were
> automatically detected.

```console
$ docker buildx inspect elated_tesla
Name:          elated_tesla
Driver:        docker-container
Last Activity: 2022-11-30 12:42:47 +0100 CET

Nodes:
Name:           elated_tesla0
Endpoint:       unix:///var/run/docker.sock
Driver Options: env.BUILDKIT_STEP_LOG_MAX_SPEED="10485760" env.JAEGER_TRACE="localhost:6831" image="moby/buildkit:latest" network="host" env.BUILDKIT_STEP_LOG_MAX_SIZE="10485760"
Status:         running
Flags:          --debug --allow-insecure-entitlement security.insecure --allow-insecure-entitlement network.host
BuildKit:       v0.10.6
Platforms:      linux/arm64*, linux/arm/v7, linux/arm/v6
Labels:
 org.mobyproject.buildkit.worker.executor:         oci
 org.mobyproject.buildkit.worker.hostname:         docker-desktop
 org.mobyproject.buildkit.worker.network:          host
 org.mobyproject.buildkit.worker.oci.process-mode: sandbox
 org.mobyproject.buildkit.worker.selinux.enabled:  false
 org.mobyproject.buildkit.worker.snapshotter:      overlayfs
GC Policy rule#0:
 All:           false
 Filters:       type==source.local,type==exec.cachemount,type==source.git.checkout
 Keep Duration: 48h0m0s
 Keep Bytes:    488.3MiB
GC Policy rule#1:
 All:           false
 Keep Duration: 1440h0m0s
 Keep Bytes:    24.21GiB
GC Policy rule#2:
 All:        false
 Keep Bytes: 24.21GiB
GC Policy rule#3:
 All:        true
 Keep Bytes: 24.21GiB
```

`debug` flag can also be used to get more information about the builder:

```console
$ docker --debug buildx inspect elated_tesla
```
