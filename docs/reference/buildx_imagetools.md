# buildx imagetools

```
docker buildx imagetools [OPTIONS] COMMAND
```

<!---MARKER_GEN_START-->
Commands to work on images in registry

### Subcommands

| Name                                      | Description                               |
|:------------------------------------------|:------------------------------------------|
| [`create`](buildx_imagetools_create.md)   | Create a new image based on source images |
| [`inspect`](buildx_imagetools_inspect.md) | Show details of an image in the registry  |


### Options

| Name                    | Type     | Default | Description                              |
|:------------------------|:---------|:--------|:-----------------------------------------|
| [`--builder`](#builder) | `string` |         | Override the configured builder instance |


<!---MARKER_GEN_END-->

## Description

Imagetools contains commands for working with manifest lists in the registry.
These commands are useful for inspecting multi-platform build results.

## Examples

### <a name="builder"></a> Override the configured builder instance (--builder)

Same as [`buildx --builder`](buildx.md#builder).
