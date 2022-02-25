# buildx imagetools create

```
docker buildx imagetools create [OPTIONS] [SOURCE] [SOURCE...]
```

<!---MARKER_GEN_START-->
Create a new image based on source images

### Options

| Name | Type | Default | Description |
| --- | --- | --- | --- |
| [`--append`](#append) |  |  | Append to existing manifest |
| [`--builder`](#builder) | `string` |  | Override the configured builder instance |
| [`--dry-run`](#dry-run) |  |  | Show final image instead of pushing |
| [`-f`](#file), [`--file`](#file) | `stringArray` |  | Read source descriptor from file |
| [`-t`](#tag), [`--tag`](#tag) | `stringArray` |  | Set reference for new image |


<!---MARKER_GEN_END-->

## Description

Imagetools contains commands for working with manifest lists in the registry.
These commands are useful for inspecting multi-platform build results.

Create a new manifest list based on source manifests. The source manifests can
be manifest lists or single platform distribution manifests and must already
exist in the registry where the new manifest is created. If only one source is
specified, create performs a carbon copy.

## Examples

### <a name="append"></a> Append new sources to an existing manifest list (--append)

Use the `--append` flag to append the new sources to an existing manifest list
in the destination.

### <a name="builder"></a> Override the configured builder instance (--builder)

Same as [`buildx --builder`](buildx.md#builder).

### <a name="dry-run"></a> Show final image instead of pushing (--dry-run)

Use the `--dry-run` flag to not push the image, just show it.

### <a name="file"></a> Read source descriptor from a file (-f, --file)

```
-f FILE or --file FILE
```

Reads source from files. A source can be a manifest digest, manifest reference,
or a JSON of OCI descriptor object.

In order to define annotations or additional platform properties like `os.version` and
`os.features` you need to add them in the OCI descriptor object encoded in JSON.

```console
$ docker buildx imagetools inspect --raw alpine | jq '.manifests[0] | .platform."os.version"="10.1"' > descr.json
$ docker buildx imagetools create -f descr.json myuser/image
```

The descriptor in the file is merged with existing descriptor in the registry if it exists.

The supported fields for the descriptor are defined in [OCI spec](https://github.com/opencontainers/image-spec/blob/master/descriptor.md#properties) .

### <a name="tag"></a> Set reference for new image  (-t, --tag)

```
-t IMAGE or --tag IMAGE
```

Use the `-t` or `--tag` flag to set the name of the image to be created.

```console
$ docker buildx imagetools create --dry-run alpine@sha256:5c40b3c27b9f13c873fefb2139765c56ce97fd50230f1f2d5c91e55dec171907 sha256:c4ba6347b0e4258ce6a6de2401619316f982b7bcc529f73d2a410d0097730204
$ docker buildx imagetools create -t tonistiigi/myapp -f image1 -f image2
```
