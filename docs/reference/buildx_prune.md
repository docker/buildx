# buildx prune

```text
docker buildx prune [OPTIONS]
```

<!---MARKER_GEN_START-->
Remove build cache

### Options

| Name                                  | Type     | Default | Description                                            |
|:--------------------------------------|:---------|:--------|:-------------------------------------------------------|
| [`-a`](#all), [`--all`](#all)         | `bool`   |         | Include internal/frontend images                       |
| [`--builder`](#builder)               | `string` |         | Override the configured builder instance               |
| `-D`, `--debug`                       | `bool`   |         | Enable debug logging                                   |
| [`--filter`](#filter)                 | `filter` |         | Provide filter values                                  |
| `-f`, `--force`                       | `bool`   |         | Do not prompt for confirmation                         |
| [`--max-used-space`](#max-used-space) | `bytes`  | `0`     | Maximum amount of disk space allowed to keep for cache |
| [`--min-free-space`](#min-free-space) | `bytes`  | `0`     | Target amount of free disk space after pruning         |
| [`--reserved-space`](#reserved-space) | `bytes`  | `0`     | Amount of disk space always allowed to keep for cache  |
| `--verbose`                           | `bool`   |         | Provide a more verbose output                          |


<!---MARKER_GEN_END-->

## Description

Clears the build cache of the selected builder.

## Examples

### <a name="all"></a> Include internal/frontend images (--all)

The `--all` flag to allow clearing internal helper images and frontend images
set using the `#syntax=` directive or the `BUILDKIT_SYNTAX` build argument.

### <a name="filter"></a> Provide filter values (--filter)

You can finely control which cache records to delete using the `--filter` flag.

The filter format is in the form of `<key><op><value>`, known as selectors. All
selectors must match the target object for the filter to be true. We define the
operators `=` for equality, `!=` for not equal and `~=` for a regular
expression.

Valid filter keys are:
- `until` flag to keep records that have been used in the last duration time.
  Value is a duration string, e.g. `24h` or `2h30m`, with allowable units of
  `(h)ours`, `(m)inutes` and `(s)econds`.
- `id` flag to target a specific image ID.
- `parents` flag to target records that are parents of the
  specified image ID. Multiple parent IDs are separated by a semicolon (`;`).
- `description` flag to target records whose description is the specified
  substring.
- `inuse` flag to target records that are actively in use and therefore not
  reclaimable.
- `mutable` flag to target records that are mutable.
- `immutable` flag to target records that are immutable.
- `shared` flag to target records that are shared with other resources,
  typically images.
- `private` flag to target records that are not shared.
- `type` flag to target records by type. Valid types are:
  - `internal`
  - `frontend`
  - `source.local`
  - `source.git.checkout`
  - `exec.cachemount`
  - `regular`

Examples:

```console
docker buildx prune --filter "until=24h"
docker buildx prune --filter "description~=golang"
docker buildx prune --filter "parents=dpetmoi6n0yqanxjqrbnofz9n;kgoj0q6g57i35gdyrv546alz7"
docker buildx prune --filter "type=source.local"
docker buildx prune --filter "type!=exec.cachemount"
```

> [!NOTE]
> Multiple `--filter` flags are ANDed together.

### <a name="max-used-space"></a> Maximum amount of disk space allowed to keep for cache (--max-used-space)

The `--max-used-space` flag allows setting a maximum amount of disk space
that the build cache can use. If the cache is using more disk space than this
value, the least recently used cache records are deleted until the total
used space is less than or equal to the specified value.

The value is specified in bytes. You can use a human-readable memory string,
e.g. `128mb`, `2gb`, etc. Units are case-insensitive.

### <a name="min-free-space"></a> Target amount of free disk space after pruning (--min-free-space)

The `--min-free-space` flag allows setting a target amount of free disk space
that should be available after pruning. If the available disk space is less
than this value, the least recently used cache records are deleted until
the available free space is greater than or equal to the specified value.

The value is specified in bytes. You can use a human-readable memory string,
e.g. `128mb`, `2gb`, etc. Units are case-insensitive.

### <a name="reserved-space"></a> Amount of disk space always allowed to keep for cache (--reserved-space)

The `--reserved-space` flag allows setting an amount of disk space that
should always be kept for the build cache. If the available disk space is less
than this value, the least recently used cache records are deleted until
the available free space is greater than or equal to the specified value.

The value is specified in bytes. You can use a human-readable memory string,
e.g. `128mb`, `2gb`, etc. Units are case-insensitive.

### <a name="builder"></a> Override the configured builder instance (--builder)

Same as [`buildx --builder`](buildx.md#builder).
