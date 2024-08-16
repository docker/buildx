# buildx use

```
docker buildx use [OPTIONS] NAME
```

<!---MARKER_GEN_START-->
Set the current builder instance

### Options

| Name                    | Type     | Default | Description                                |
|:------------------------|:---------|:--------|:-------------------------------------------|
| [`--builder`](#builder) | `string` |         | Override the configured builder instance   |
| `-D`, `--debug`         | `bool`   |         | Enable debug logging                       |
| `--default`             | `bool`   |         | Set builder as default for current context |
| `--global`              | `bool`   |         | Builder persists context changes           |


<!---MARKER_GEN_END-->

## Description

Switches the current builder instance. Build commands invoked after this command
will run on a specified builder. Alternatively, a context name can be used to
switch to the default builder of that context.

## Examples

### <a name="builder"></a> Override the configured builder instance (--builder)

Same as [`buildx --builder`](buildx.md#builder).
