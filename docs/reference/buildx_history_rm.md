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
remove specific builds by ID or offset, or delete all records at once using
the `--all` flag.

## Examples

### <a name="remove-specific-build"></a> Remove a specific build

```console
# Using a build ID
docker buildx history rm qu2gsuo8ejqrwdfii23xkkckt

# Or using a relative offset
docker buildx history rm ^1
```

### <a name="remove-multiple-builds"></a> Remove multiple builds

```console
# Using build IDs
docker buildx history rm qu2gsuo8ejqrwdfii23xkkckt qsiifiuf1ad9pa9qvppc0z1l3

# Or using relative offsets
docker buildx history rm ^1 ^2
```

### <a name="remove-all-build-records"></a> Remove all build records from the current builder

```console
docker buildx history rm --all
```
