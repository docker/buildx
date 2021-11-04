# buildx stop

```
docker buildx stop [NAME]
```

<!---MARKER_GEN_START-->
Stop builder instance

### Options

| Name | Description |
| --- | --- |
| [`--builder string`](#builder) | Override the configured builder instance |


<!---MARKER_GEN_END-->

## Description

Stops the specified or current builder. This will not prevent buildx build to
restart the builder. The implementation of stop depends on the driver.

## Examples

### <a name="builder"></a> Override the configured builder instance (--builder)

Same as [`buildx --builder`](buildx.md#builder).
