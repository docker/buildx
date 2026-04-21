# docker buildx replay snapshot

<!---MARKER_GEN_START-->
Export replay inputs for a subject as a reusable materials store

### Options

| Name                  | Type          | Default   | Description                                                                                                                                                          |
|:----------------------|:--------------|:----------|:---------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `--builder`           | `string`      |           | Override the configured builder instance                                                                                                                             |
| `-D`, `--debug`       | `bool`        |           | Enable debug logging                                                                                                                                                 |
| `--dry-run`           | `bool`        |           | Print a JSON plan of the snapshot without writing output                                                                                                             |
| `--include-materials` | `bool`        | `true`    | Include material content in the snapshot                                                                                                                             |
| `--materials`         | `stringArray` |           | Materials store (repeatable; format: `provenance` \| `registry://<ref>` \| `oci-layout://<path>[:<tag>]` \| `<absolute-path>` \| `<key>=<value>`)                    |
| `--network`           | `string`      | `default` | Network mode for RUN instructions (`default` \| `none`)                                                                                                              |
| `-o`, `--output`      | `stringArray` |           | Output destination (default: `-` — oci tar to stdout; bare `<path>` writes an oci-layout directory; `type=oci,dest=X[,tar=true\|false]`; `type=registry,name=<ref>`) |
| `--platform`          | `stringArray` |           | Subjects to replay (defaults to the current host platform; `all` keeps every platform)                                                                               |
| `--progress`          | `string`      | `auto`    | Set type of progress output (`auto` \| `plain` \| `tty` \| `quiet` \| `rawjson`)                                                                                     |
| `--secret`            | `stringArray` |           | Secret to expose to the replayed build (format: `id=mysecret[,src=/local/secret]`)                                                                                   |
| `--ssh`               | `stringArray` |           | SSH agent socket or keys to expose (format: `default\|<id>[=<socket>\|<key>[,<key>]]`)                                                                               |


<!---MARKER_GEN_END-->

## Description

`replay snapshot` packages the provenance predicate, the attestation
manifest, and every recorded material into a self-contained OCI index that
can later be used as `--materials=<snapshot>` for `replay build` or
`replay verify`.

The snapshot is an OCI image-spec 1.1 index:

- `artifactType = application/vnd.docker.buildx.snapshots.v1+json`
- `subject` points at the original provenance attestation manifest.
- `manifests[0]` is a materials artifact manifest whose layers hold the
  http / container-blob materials plus an opaque copy of each image
  material's root index.
- Remaining `manifests[]` entries are per-image-material platform
  manifests.

For multi-platform subjects, `replay snapshot` emits an outer OCI index that
wraps one per-platform snapshot per architecture.

## Examples

### Local OCI layout

```console
docker buildx replay snapshot docker-image://example.com/app@sha256:deadbeef \
  --output=type=local,dest=./my-snapshot
```

### Push to a registry

```console
docker buildx replay snapshot docker-image://example.com/app@sha256:deadbeef \
  --output=type=registry,name=registry.example.com/snapshots/app:latest
```

### OCI tar

```console
docker buildx replay snapshot docker-image://example.com/app@sha256:deadbeef \
  --output=type=oci,dest=./snapshot.oci.tar
```
