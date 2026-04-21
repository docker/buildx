# docker buildx replay build

<!---MARKER_GEN_START-->
Rebuild an image from provenance and pinned materials

### Options

| Name             | Type          | Default     | Description                                                                                                                                       |
|:-----------------|:--------------|:------------|:--------------------------------------------------------------------------------------------------------------------------------------------------|
| `--builder`      | `string`      |             | Override the configured builder instance                                                                                                          |
| `-D`, `--debug`  | `bool`        |             | Enable debug logging                                                                                                                              |
| `--dry-run`      | `bool`        |             | Print a JSON plan of the replay without solving or exporting                                                                                      |
| `--load`         | `bool`        |             | Shorthand for `--output=type=docker`                                                                                                              |
| `--materials`    | `stringArray` |             | Materials store (repeatable; format: `provenance` \| `registry://<ref>` \| `oci-layout://<path>[:<tag>]` \| `<absolute-path>` \| `<key>=<value>`) |
| `--network`      | `string`      | `default`   | Network mode for RUN instructions (`default` \| `none`)                                                                                           |
| `-o`, `--output` | `stringArray` |             | Output destination (format: `type=local,dest=path`)                                                                                               |
| `--platform`     | `stringArray` |             | Subjects to replay (defaults to the current host platform; `all` keeps every platform)                                                            |
| `--progress`     | `string`      | `auto`      | Set type of progress output (`auto` \| `plain` \| `tty` \| `quiet` \| `rawjson`)                                                                  |
| `--push`         | `bool`        |             | Shorthand for `--output=type=registry,unpack=false`                                                                                               |
| `--replay-mode`  | `string`      | `materials` | Replay mode (`materials` \| `frontend` \| `llb`)                                                                                                  |
| `--secret`       | `stringArray` |             | Secret to expose to the replayed build (format: `id=mysecret[,src=/local/secret]`)                                                                |
| `--ssh`          | `stringArray` |             | SSH agent socket or keys to expose (format: `default\|<id>[=<socket>\|<key>[,<key>]]`)                                                            |
| `-t`, `--tag`    | `stringArray` |             | Image identifier (format: `[registry/]repository[:tag]`)                                                                                          |


<!---MARKER_GEN_END-->

## Description

`replay build` reconstructs an image from the provenance attestation attached
to an existing subject. The default mode (`materials`) enforces strict source
pinning via BuildKit's session source-policy callback — every resolution
must match the digest recorded in `resolvedDependencies` or the solve fails.

## Examples

### Replay a registry image and export to an OCI tar

```console
docker buildx replay build docker-image://example.com/app@sha256:deadbeef \
  --output=type=oci,dest=replay.oci.tar
```

### Dry-run a replay to inspect the plan

```console
docker buildx replay build docker-image://example.com/app@sha256:deadbeef --dry-run | jq
```

### Use a pre-pinned snapshot as the materials store

```console
docker buildx replay build docker-image://example.com/app@sha256:deadbeef \
  --materials=oci-layout:///path/to/snapshot \
  --output=type=oci,dest=replay.oci.tar
```

## Exit codes

`replay build` maps typed errors to stable exit codes so CI tooling can
react deterministically. See `replay` documentation for the full list.
