# docker buildx replay build

<!---MARKER_GEN_START-->
Rebuild an image from provenance and pinned materials

### Options

| Name             | Type          | Default     | Description                                                                                                      |
|:-----------------|:--------------|:------------|:-----------------------------------------------------------------------------------------------------------------|
| `--builder`      | `string`      |             | Override the configured builder instance                                                                         |
| `-D`, `--debug`  | `bool`        |             | Enable debug logging                                                                                             |
| `--dry-run`      | `bool`        |             | Print a plan of the replay without solving or exporting                                                          |
| `--format`       | `string`      | `pretty`    | Format dry-run output (`pretty` \| `json`)                                                                       |
| `--load`         | `bool`        |             | Shorthand for `--output=type=docker`                                                                             |
| `--network`      | `string`      |             | Network mode for RUN instructions (`default` \| `none`; defaults to the mode of the original build)              |
| `-o`, `--output` | `stringArray` |             | Output destination (format: `type=local,dest=path`)                                                              |
| `--platform`     | `stringArray` |             | Platform of the subject to replay (defaults to the only platform of the subject or the builder default platform) |
| `--progress`     | `string`      | `auto`      | Set type of progress output (`auto` \| `plain` \| `tty` \| `quiet` \| `rawjson`)                                 |
| `--push`         | `bool`        |             | Shorthand for `--output=type=registry,unpack=false`                                                              |
| `--replay-mode`  | `string`      | `materials` | Replay mode (`materials` \| `frontend`)                                                                          |
| `--secret`       | `stringArray` |             | Secret to expose to the replayed build (format: `id=mysecret[,src=/local/secret]`)                               |
| `--ssh`          | `stringArray` |             | SSH agent socket or keys to expose (format: `default\|<id>[=<socket>\|<key>[,<key>]]`)                           |
| `-t`, `--tag`    | `stringArray` |             | Image identifier (format: `[registry/]repository[:tag]`)                                                         |


<!---MARKER_GEN_END-->

## Description

`replay build` reconstructs an image from the provenance attestation attached
to an existing subject.

The replay mode controls how sources are resolved:

- `materials` (default) pins every source to the digest recorded in the
  provenance. A source that is not recorded, or whose content changed, fails
  the build.
- `frontend` replays the recorded frontend and options, but resolves sources
  again, so the result can differ from the original build.

Replayed builds do not add new provenance or SBOM attestations. Local outputs
with `mode=delete` are not supported.

## Examples

### Replay a registry image and export to an OCI tar

```console
docker buildx replay build docker-image://example.com/app@sha256:deadbeef \
  --output=type=oci,dest=replay.oci.tar
```

### Dry-run a replay to inspect the plan

```console
docker buildx replay build docker-image://example.com/app@sha256:deadbeef --dry-run --format=json | jq
```

Dry-run runs the same checks as a real replay, so a subject that cannot be
replayed fails before any build starts.

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
