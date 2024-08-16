# buildx stop

```
docker buildx stop [NAME]
```

<!---MARKER_GEN_START-->
Stop builder instance

### Options

| Name                    | Type     | Default | Description                              |
|:------------------------|:---------|:--------|:-----------------------------------------|
| [`--builder`](#builder) | `string` |         | Override the configured builder instance |
| `-D`, `--debug`         | `bool`   |         | Enable debug logging                     |


<!---MARKER_GEN_END-->

## Description

Stops the specified or current builder. This does not prevent buildx build to
restart the builder. The implementation of stop depends on the driver.

## Examples

### <a name="builder"></a> Override the configured builder instance (--builder)

Same as [`buildx --builder`](buildx.md#builder).
