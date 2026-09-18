# docker buildx replay snapshot

<!---MARKER_GEN_START-->
Export replay inputs for a subject as a reusable materials store

### Options

| Name                  | Type          | Default   | Description                                                                                                                                       |
|:----------------------|:--------------|:----------|:--------------------------------------------------------------------------------------------------------------------------------------------------|
| `--builder`           | `string`      |           | Override the configured builder instance                                                                                                          |
| `-D`, `--debug`       | `bool`        |           | Enable debug logging                                                                                                                              |
| `--dry-run`           | `bool`        |           | Print a JSON plan of the snapshot without writing output                                                                                          |
| `--include-materials` | `bool`        | `true`    | Include material content in the snapshot                                                                                                          |
| `--materials`         | `stringArray` |           | Materials store (repeatable; format: `provenance` \| `registry://<ref>` \| `oci-layout://<path>[:<tag>]` \| `<absolute-path>` \| `<key>=<value>`) |
| `--network`           | `string`      | `default` | Network mode for RUN instructions (`default` \| `none`)                                                                                           |
| `-o`, `--output`      | `stringArray` |           | Output destination (default: `-` — oci tar to stdout; bare `<path>` writes an oci-layout directory; `type=oci,dest=X[,tar=true\|false]`)          |
| `--platform`          | `stringArray` |           | Subjects to replay (defaults to the current host platform; `all` keeps every platform)                                                            |
| `--progress`          | `string`      | `auto`    | Set type of progress output (`auto` \| `plain` \| `tty` \| `quiet` \| `rawjson`)                                                                  |
| `--secret`            | `stringArray` |           | Secret to expose to the replayed build (format: `id=mysecret[,src=/local/secret]`)                                                                |
| `--ssh`               | `stringArray` |           | SSH agent socket or keys to expose (format: `default\|<id>[=<socket>\|<key>[,<key>]]`)                                                            |


<!---MARKER_GEN_END-->

## Description

`replay snapshot` packages the provenance predicate, the attestation manifest,
and every resolved material into a self-contained OCI index. Snapshot-backed
input injection into `replay build` and `replay verify` is not implemented yet;
those commands reject explicit material stores rather than silently fetching
from the network.

The default `provenance` material resolver can fetch image materials, but does
not yet fetch recorded HTTP or Git materials. Builds with remote contexts must
provide those bytes with explicit `<uri>=<absolute-path>` material overrides
when creating a snapshot.

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
  --materials=provenance \
  --materials=https://example.com/context.tar=/path/to/context.tar \
  --output=type=local,dest=./my-snapshot
```

### OCI tar

```console
docker buildx replay snapshot docker-image://example.com/app@sha256:deadbeef \
  --materials=provenance \
  --materials=https://example.com/context.tar=/path/to/context.tar \
  --output=type=oci,dest=./snapshot.oci.tar
```
