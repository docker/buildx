# buildx prune

```
docker buildx prune
```

<!---MARKER_GEN_START-->
Remove build cache

### Options

| Name | Type | Default | Description |
| --- | --- | --- | --- |
| `-a`, `--all` |  |  | Remove all unused images, not just dangling ones |
| [`--builder`](#builder) | `string` |  | Override the configured builder instance |
| `--filter` | `filter` |  | Provide filter values (e.g., `until=24h`) |
| `-f`, `--force` |  |  | Do not prompt for confirmation |
| `--keep-storage` | `bytes` | `0` | Amount of disk space to keep for cache |
| `--verbose` |  |  | Provide a more verbose output |


<!---MARKER_GEN_END-->

## Examples

### <a name="builder"></a> Override the configured builder instance (--builder)

Same as [`buildx --builder`](buildx.md#builder).
