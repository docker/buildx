# docker buildx replay verify

<!---MARKER_GEN_START-->
Replay a subject and compare the result against the original artifact

### Options

| Name             | Type          | Default   | Description                                                                                                                                       |
|:-----------------|:--------------|:----------|:--------------------------------------------------------------------------------------------------------------------------------------------------|
| `--builder`      | `string`      |           | Override the configured builder instance                                                                                                          |
| `--compare`      | `string`      | `digest`  | Comparison mode (`digest` \| `artifact` \| `semantic`)                                                                                            |
| `-D`, `--debug`  | `bool`        |           | Enable debug logging                                                                                                                              |
| `--materials`    | `stringArray` |           | Materials store (repeatable; format: `provenance` \| `registry://<ref>` \| `oci-layout://<path>[:<tag>]` \| `<absolute-path>` \| `<key>=<value>`) |
| `--network`      | `string`      | `default` | Network mode for RUN instructions (`default` \| `none`)                                                                                           |
| `-o`, `--output` | `stringArray` |           | Output destination for the verification result (VSA) (format: `type=local,dest=path` \| `type=oci,dest=file` \| `type=attest`)                    |
| `--platform`     | `stringArray` |           | Subjects to replay (defaults to the current host platform; `all` keeps every platform)                                                            |
| `--progress`     | `string`      | `auto`    | Set type of progress output (`auto` \| `plain` \| `tty` \| `quiet` \| `rawjson`)                                                                  |
| `--secret`       | `stringArray` |           | Secret to expose to the replayed build (format: `id=mysecret[,src=/local/secret]`)                                                                |
| `--ssh`          | `stringArray` |           | SSH agent socket or keys to expose (format: `default\|<id>[=<socket>\|<key>[,<key>]]`)                                                            |


<!---MARKER_GEN_END-->

## Description

`replay verify` replays the subject to an ephemeral OCI layout and compares
the result against the original. Three comparison modes are supported:

- `digest` (default) — manifest-digest equality. Cheapest; passes only on
  byte-for-byte reproducibility.
- `artifact` — walk both content stores and produce a JSON divergence
  report using a very basic event tree comparator intended for demo use.
- `semantic` — not implemented in v1.

`artifact` mode currently avoids the `diffoci` package because that dependency
still requires older containerd plumbing and private `linkname`-based linking.
TODO: experiment with `diffoci` again once those constraints are gone.

On mismatch the command exits with code 8 (`CompareMismatchError`).

When `--output` is set, verify emits a SLSA Verification Summary Attestation
(predicate type `https://slsa.dev/verification_summary/v1`) and — in
artifact mode — a sidecar diff report.

## Output formats

- `type=local,dest=<dir>` — writes `vsa.intoto.jsonl` and (for artifact
  mode) `diff.json` into `<dir>`.
- `type=oci,dest=<file>` — packages both blobs as an OCI artifact with
  `artifactType = application/vnd.docker.buildx.snapshots.verify.v1+json`.
- `type=attest` — attaches the VSA as a referrer on the subject in the
  registry (only valid when the subject is a registry image).

## Examples

### Digest compare against a registry image

```console
docker buildx replay verify docker-image://example.com/app@sha256:deadbeef
```

### Artifact compare and persist the VSA locally

```console
docker buildx replay verify docker-image://example.com/app@sha256:deadbeef \
  --compare=artifact \
  --output=type=local,dest=./verify-out
```
