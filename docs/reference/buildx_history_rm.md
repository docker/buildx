# docker buildx history rm

<!---MARKER_GEN_START-->
Remove build records

### Options

| Name            | Type     | Default | Description                              |
|:----------------|:---------|:--------|:-----------------------------------------|
| `--all`         | `bool`   |         | Remove all build records                 |
| `--builder`     | `string` |         | Override the configured builder instance |
| `-D`, `--debug` | `bool`   |         | Enable debug logging                     |


<!---MARKER_GEN_END-->

## Description

Remove one or more build records from the current builder’s history. You can
remove specific builds by name or ID, or delete all records at once using
the `--all` flag.

## Examples

### Remove a specific build

```console
docker buildx history rm mybuild
```

### Remove multiple builds

```console
docker buildx history rm mybuild frontend-build backend-build
```

### Remove all build records from the current builder

```console
docker buildx history rm --all
```
