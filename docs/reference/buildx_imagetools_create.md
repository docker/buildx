# buildx imagetools create

```
Usage:  docker buildx imagetools create [OPTIONS] [SOURCE] [SOURCE...]

Create a new image based on source images

Options:
      --append             Append to existing manifest
      --builder string     Override the configured builder instance
      --dry-run            Show final image instead of pushing
  -f, --file stringArray   Read source descriptor from file
  -t, --tag stringArray    Set reference for new image
```

## Description

Imagetools contains commands for working with manifest lists in the registry.
These commands are useful for inspecting multi-platform build results.

Create creates a new manifest list based on source manifests. The source
manifests can be manifest lists or single platform distribution manifests and
must already exist in the registry where the new manifest is created. If only
one source is specified create performs a carbon copy.

## Examples

### Append new sources to an existing manifest list (--append)

Use the `--append` flag to append the new sources to an existing manifest list
in the destination.

### Show final image instead of pushing (--dry-run)

Use the `--dry-run` flag to not push the image, just show it.

### Read source descriptor from a file (-f, --file)

```
-f FILE or --file FILE
```

Reads source from files. A source can be a manifest digest, manifest reference,
or a JSON of OCI descriptor object.

### Set reference for new image  (-t, --tag)

```
-t IMAGE or --tag IMAGE
```

Use the `-t` or `--tag` flag to set the name of the image to be created.

**Examples**

```console
$ docker buildx imagetools create --dry-run alpine@sha256:5c40b3c27b9f13c873fefb2139765c56ce97fd50230f1f2d5c91e55dec171907 sha256:c4ba6347b0e4258ce6a6de2401619316f982b7bcc529f73d2a410d0097730204

$ docker buildx imagetools create -t tonistiigi/myapp -f image1 -f image2
```
