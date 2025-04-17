# docker buildx history logs

<!---MARKER_GEN_START-->
Print the logs of a build

### Options

| Name            | Type     | Default | Description                                       |
|:----------------|:---------|:--------|:--------------------------------------------------|
| `--builder`     | `string` |         | Override the configured builder instance          |
| `-D`, `--debug` | `bool`   |         | Enable debug logging                              |
| `--progress`    | `string` | `plain` | Set type of progress output (plain, rawjson, tty) |


<!---MARKER_GEN_END-->

## Description

Print the logs for a completed build. The output appears in the same format as `--progress=plain`, showing the full logs for each step without multiplexing.

By default, this shows logs for the most recent build on the current builder.

## Examples

### Print logs for the most recent build

```console
docker buildx history logs
```

### Print logs for a specific build

```console
docker buildx history logs mybuild
```

### Print logs in JSON format

```console
docker buildx history logs mybuild --progress rawjson
```

### Print logs in TTY format

```console
docker buildx history logs mybuild --progress tty
```
