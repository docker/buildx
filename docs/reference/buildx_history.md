# docker buildx history

```text
docker buildx history [OPTIONS] COMMAND
```

<!---MARKER_GEN_START-->
Commands to work on build records

### Subcommands

| Name                                   | Description                                     |
|:---------------------------------------|:------------------------------------------------|
| [`export`](buildx_history_export.md)   | Export build records into Docker Desktop bundle |
| [`import`](buildx_history_import.md)   | Import build records into Docker Desktop        |
| [`inspect`](buildx_history_inspect.md) | Inspect a build record                          |
| [`logs`](buildx_history_logs.md)       | Print the logs of a build record                |
| [`ls`](buildx_history_ls.md)           | List build records                              |
| [`open`](buildx_history_open.md)       | Open a build record in Docker Desktop           |
| [`rm`](buildx_history_rm.md)           | Remove build records                            |
| [`trace`](buildx_history_trace.md)     | Show the OpenTelemetry trace of a build record  |


### Options

| Name            | Type     | Default | Description                              |
|:----------------|:---------|:--------|:-----------------------------------------|
| `--builder`     | `string` |         | Override the configured builder instance |
| `-D`, `--debug` | `bool`   |         | Enable debug logging                     |


<!---MARKER_GEN_END-->

### Build references

Most `buildx history` subcommands accept a build reference to identify which
build to act on. You can specify the build in two ways:

- By build ID, fetched by `docker buildx history ls`:

    ```console
    docker buildx history export qu2gsuo8ejqrwdfii23xkkckt --output build.dockerbuild
    ```

- By relative offset, to refer to recent builds:

    ```console
    docker buildx history export ^1 --output build.dockerbuild
    ```

    - `^0` or no reference targets the most recent build
    - `^1` refers to the build before the most recent
    - `^2` refers to two builds back, and so on

Offset references are supported in the following `buildx history` commands:

- `logs`
- `inspect`
- `open`
- `trace`
- `export`
- `rm`
