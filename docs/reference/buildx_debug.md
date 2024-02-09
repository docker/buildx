# docker buildx debug

<!---MARKER_GEN_START-->
Start debugger

### Subcommands

| Name                             | Description   |
|:---------------------------------|:--------------|
| [`build`](buildx_debug_build.md) | Start a build |


### Options

| Name              | Type     | Default | Description                                                                                              |
|:------------------|:---------|:--------|:---------------------------------------------------------------------------------------------------------|
| `--builder`       | `string` |         | Override the configured builder instance                                                                 |
| `--detach`        | `bool`   | `true`  | Detach buildx server for the monitor (supported only on linux)                                           |
| `--invoke`        | `string` |         | Launch a monitor with executing specified command                                                        |
| `--on`            | `string` | `error` | When to launch the monitor ([always, error])                                                             |
| `--progress`      | `string` | `auto`  | Set type of progress output (`auto`, `plain`, `tty`) for the monitor. Use plain to show container output |
| `--root`          | `string` |         | Specify root directory of server to connect for the monitor                                              |
| `--server-config` | `string` |         | Specify buildx server config file for the monitor (used only when launching new server)                  |


<!---MARKER_GEN_END-->

