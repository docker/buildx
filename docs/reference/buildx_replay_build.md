# docker buildx replay build

<!---MARKER_GEN_START-->
Rebuild an image from provenance and pinned materials

### Options

| Name             | Type          | Default     | Description                                                                                                                                       |
|:-----------------|:--------------|:------------|:--------------------------------------------------------------------------------------------------------------------------------------------------|
| `--builder`      | `string`      |             | Override the configured builder instance                                                                                                          |
| `-D`, `--debug`  | `bool`        |             | Enable debug logging                                                                                                                              |
| `--dry-run`      | `bool`        |             | Print a plan of the replay without solving or exporting                                                                                           |
| `--format`       | `string`      | `pretty`    | Format dry-run output (`pretty` \| `json`)                                                                                                        |
| `--load`         | `bool`        |             | Shorthand for `--output=type=docker`                                                                                                              |
| `--materials`    | `stringArray` |             | Materials store (repeatable; format: `provenance` \| `registry://<ref>` \| `oci-layout://<path>[:<tag>]` \| `<absolute-path>` \| `<key>=<value>`) |
| `--network`      | `string`      | `default`   | Network mode for RUN instructions (`default` \| `none`)                                                                                           |
| `-o`, `--output` | `stringArray` |             | Output destination (format: `type=local,dest=path`)                                                                                               |
| `--platform`     | `stringArray` |             | Subjects to replay (comma-separated or repeated; defaults to the current host platform; `all` keeps every platform)                               |
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

Snapshot-backed `--materials` injection is not implemented yet. Explicit
material stores and overrides are rejected instead of silently falling back to
network sources.

## Signature verification

For image subjects, replay discovers Sigstore signatures attached to the
selected platform's provenance attestation through OCI referrers. Standalone
Sigstore bundle files are also accepted. Replay verifies the certificate,
transparency-log inclusion, observer timestamp, and signed payload before using
the provenance. Pretty and JSON dry-run output report the verified identity.

Unsigned provenance remains accepted. If a published signature is discovered
but is invalid, replay fails instead of silently treating it as unsigned.
There is not yet a `--require-signature` option or signer-authorization policy.
For a standalone bundle, verification authenticates the signed statement and
its claimed subject digest; it does not compare that digest with a separately
supplied artifact.
