# OCI exporter

The `oci` exporter outputs the build result into an
[OCI image layout](https://github.com/opencontainers/image-spec/blob/main/image-layout.md)
tarball.

The `docker` exporter behaves the same, however, it exports a docker image
layout instead.

## Synopsis

Build a container image using the `image` exporter:

```console
$ docker buildx build --output type=image[,parameters] .
```

The following table describes the available parameters that you can pass to
`--output` for `type=image`:

| Parameter         | Value            | Default                                 | Description                                                                                                                          |
| ----------------- | ---------------- | --------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------ |
| `name` | String | | Specify image name(s) |
| `compression` | `uncompressed`,`gzip`,`estargz`,`zstd` | `gzip` | Compression type, see [compression][1] |
| `compression-level` | `0..22` | | Compression level, see [compression][1] |
| `force-compression` | `true`,`false` | `false` | Forcefully apply compression, see [compression][1] |
| `oci-mediatypes` | `true`,`false` | `false` | Use OCI mediatypes in exporter manifests, see [OCI Media types][2] |
| `buildinfo` | `true`,`false` | `true` | Attach inline [build info][3] |
| `buildinfo-attrs` | `true`,`false` | `false` | Attach inline [build info attributes][3] |
| `annotation.<key>` | String | | Attach an annotation with the respective `key` and `value` to the built image,see [annotations][4] |

[1]: index.md#cache-compression
[2]: index.md#oci-media-types
[3]: index.md#build-info
[4]: #annotations

## Annotations

The annotation options are the same as for the [`image` exporter](image.md#annotations).

## Further reading
