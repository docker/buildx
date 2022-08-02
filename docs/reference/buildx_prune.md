# buildx prune

```
docker buildx prune
```

<!---MARKER_GEN_START-->
Remove build cache

### Options

| Name | Type | Default | Description |
| --- | --- | --- | --- |
| `-a`, `--all` |  |  | Include internal/frontend images |
| [`--builder`](#builder) | `string` |  | Override the configured builder instance |
| `--filter` | `filter` |  | Provide filter values (e.g., `until=24h`) |
| `-f`, `--force` |  |  | Do not prompt for confirmation |
| `--keep-storage` | `bytes` | `0` | Amount of disk space to keep for cache |
| `--verbose` |  |  | Provide a more verbose output |


<!---MARKER_GEN_END-->

## Description

Clears the build cache of the selected builder.

You can finely control what cache data is kept using:

- The `--filter=until=<duration>` flag to keep images that have been used in
  the last `<duration>` time.

  `<duration>` is a duration string, e.g. `24h` or `2h30m`, with allowable
  units of `(h)ours`, `(m)inutes` and `(s)econds`.

- The `--keep-storage=<size>` flag to keep `<size>` bytes of data in the cache.

  `<size>` is a human-readable memory string, e.g. `128mb`, `2gb`, etc. Units
  are case-insensitive.

- The `--all` flag to allow clearing internal helper images and frontend images
  set using the `#syntax=` directive or the `BUILDKIT_SYNTAX` build argument.

## Examples

### <a name="builder"></a> Override the configured builder instance (--builder)

Same as [`buildx --builder`](buildx.md#builder).
