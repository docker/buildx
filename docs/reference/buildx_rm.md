# buildx rm

```
docker buildx rm [NAME]
```

<!---MARKER_GEN_START-->
Remove a builder instance

### Options

| Name | Description |
| --- | --- |
| [`--builder string`](#builder) | Override the configured builder instance |
| [`--keep-state`](#keep-state) | Keep BuildKit state |


<!---MARKER_GEN_END-->

## Description

Removes the specified or current builder. It is a no-op attempting to remove the
default builder.

## Examples

### <a name="builder"></a> Override the configured builder instance (--builder)

Same as [`buildx --builder`](buildx.md#builder).

### <a name="keep-state"></a> Keep BuildKit state (--keep-state)

Keep BuildKit state, so it can be reused by a new builder with the same name.
Currently, only supported by the [`docker-container` driver](buildx_create.md#driver).
