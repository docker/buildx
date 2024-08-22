# docker buildx debug

<!---MARKER_GEN_START-->
Start debugger (EXPERIMENTAL)

### Subcommands

| Name                             | Description   |
|:---------------------------------|:--------------|
| [`build`](buildx_debug_build.md) | Start a build |


### Options

| Name              | Type     | Default | Description                                                                                                         |
|:------------------|:---------|:--------|:--------------------------------------------------------------------------------------------------------------------|
| `--builder`       | `string` |         | Override the configured builder instance                                                                            |
| `-D`, `--debug`   | `bool`   |         | Enable debug logging                                                                                                |
| `--detach`        | `bool`   | `true`  | Detach buildx server for the monitor (supported only on linux) (EXPERIMENTAL)                                       |
| `--invoke`        | `string` |         | Launch a monitor with executing specified command (EXPERIMENTAL)                                                    |
| `--on`            | `string` | `error` | When to launch the monitor ([always, error]) (EXPERIMENTAL)                                                         |
| `--progress`      | `string` | `auto`  | Set type of progress output (`auto`, `plain`, `tty`, `rawjson`) for the monitor. Use plain to show container output |
| `--root`          | `string` |         | Specify root directory of server to connect for the monitor (EXPERIMENTAL)                                          |
| `--server-config` | `string` |         | Specify buildx server config file for the monitor (used only when launching new server) (EXPERIMENTAL)              |


<!---MARKER_GEN_END-->

