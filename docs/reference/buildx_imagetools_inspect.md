# buildx imagetools inspect

```
Usage:  docker buildx imagetools inspect [OPTIONS] NAME

Show details of image in the registry

Options:
      --builder string   Override the configured builder instance
      --raw              Show original JSON manifest
```

## Description

Show details of image in the registry.

Example:

```console
$ docker buildx imagetools inspect alpine

Name:      docker.io/library/alpine:latest
MediaType: application/vnd.docker.distribution.manifest.list.v2+json
Digest:    sha256:28ef97b8686a0b5399129e9b763d5b7e5ff03576aa5580d6f4182a49c5fe1913

Manifests:
  Name:      docker.io/library/alpine:latest@sha256:5c40b3c27b9f13c873fefb2139765c56ce97fd50230f1f2d5c91e55dec171907
  MediaType: application/vnd.docker.distribution.manifest.v2+json
  Platform:  linux/amd64

  Name:      docker.io/library/alpine:latest@sha256:c4ba6347b0e4258ce6a6de2401619316f982b7bcc529f73d2a410d0097730204
  MediaType: application/vnd.docker.distribution.manifest.v2+json
  Platform:  linux/arm/v6
 ...
```

### Show original, unformatted JSON manifest (--raw)

Use the `--raw` option to print the original JSON bytes instead of the formatted
output.
