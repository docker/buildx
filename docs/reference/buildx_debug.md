# docker buildx debug

<!---MARKER_GEN_START-->
Start debugger (EXPERIMENTAL)

### Subcommands

| Name                             | Description   |
|:---------------------------------|:--------------|
| [`build`](buildx_debug_build.md) | Start a build |


### Options

| Name            | Type     | Default | Description                                                                                                         |
|:----------------|:---------|:--------|:--------------------------------------------------------------------------------------------------------------------|
| `--builder`     | `string` |         | Override the configured builder instance                                                                            |
| `-D`, `--debug` | `bool`   |         | Enable debug logging                                                                                                |
| `--invoke`      | `string` |         | Launch a monitor with executing specified command (EXPERIMENTAL)                                                    |
| `--on`          | `string` | `error` | When to launch the monitor ([always, error]) (EXPERIMENTAL)                                                         |
| `--progress`    | `string` | `auto`  | Set type of progress output (`auto`, `plain`, `tty`, `rawjson`) for the monitor. Use plain to show container output |


<!---MARKER_GEN_END-->

