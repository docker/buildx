# buildx

```text
docker buildx [OPTIONS] COMMAND
```

<!---MARKER_GEN_START-->
Extended build capabilities with BuildKit

### Subcommands

| Name                                 | Description                                     |
|:-------------------------------------|:------------------------------------------------|
| [`bake`](buildx_bake.md)             | Build from a file                               |
| [`build`](buildx_build.md)           | Start a build                                   |
| [`create`](buildx_create.md)         | Create a new builder instance                   |
| [`debug`](buildx_debug.md)           | Start debugger (EXPERIMENTAL)                   |
| [`dial-stdio`](buildx_dial-stdio.md) | Proxy current stdio streams to builder instance |
| [`du`](buildx_du.md)                 | Disk usage                                      |
| [`history`](buildx_history.md)       | Commands to work on build records               |
| [`imagetools`](buildx_imagetools.md) | Commands to work on images in registry          |
| [`inspect`](buildx_inspect.md)       | Inspect current builder instance                |
| [`ls`](buildx_ls.md)                 | List builder instances                          |
| [`prune`](buildx_prune.md)           | Remove build cache                              |
| [`rm`](buildx_rm.md)                 | Remove one or more builder instances            |
| [`stop`](buildx_stop.md)             | Stop builder instance                           |
| [`use`](buildx_use.md)               | Set the current builder instance                |
| [`version`](buildx_version.md)       | Show buildx version information                 |


### Options

| Name                    | Type     | Default | Description                              |
|:------------------------|:---------|:--------|:-----------------------------------------|
| [`--builder`](#builder) | `string` |         | Override the configured builder instance |
| `-D`, `--debug`         | `bool`   |         | Enable debug logging                     |


<!---MARKER_GEN_END-->

## Examples

### <a name="builder"></a> Override the configured builder instance (--builder)

You can also use the `BUILDX_BUILDER` environment variable.
