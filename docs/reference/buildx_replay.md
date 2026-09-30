# docker buildx replay

```text
docker buildx replay [OPTIONS] COMMAND
```

<!---MARKER_GEN_START-->
Replay a build from its provenance (EXPERIMENTAL)

### Subcommands

| Name                              | Description                                           |
|:----------------------------------|:------------------------------------------------------|
| [`build`](buildx_replay_build.md) | Rebuild an image from provenance and pinned materials |


### Options

| Name            | Type     | Default | Description                              |
|:----------------|:---------|:--------|:-----------------------------------------|
| `--builder`     | `string` |         | Override the configured builder instance |
| `-D`, `--debug` | `bool`   |         | Enable debug logging                     |


<!---MARKER_GEN_END-->

## Description

`buildx replay` reads the SLSA provenance attestation of an existing build and
reproduces the build with the recorded frontend, options, and source digests.
Replay runs on the selected builder and requires BuildKit v0.27 or later.

Subjects are accepted in three forms:

- `docker-image://<ref>` or a bare `<ref>` — resolve through the registry.
- `oci-layout://<path>[:<tag>]` — read from a local OCI layout.
- A local attestation file: an in-toto statement (`.intoto.jsonl`), an
  unsigned DSSE envelope, a Sigstore bundle, or a bare SLSA provenance
  predicate.

A build can be replayed when:

- its provenance was recorded with `mode=max`
  (`--provenance=mode=max` or `--attest=type=provenance,mode=max`). `mode=min`
  provenance omits the build arguments, secrets, and SSH needed for replay;
- its build context was a Git repository or an HTTP(S) URL. Builds that used
  local directories, stdin, OCI layouts, or other Bake targets as build
  contexts cannot be replayed;
- it did not use `--network=host`, unless a different `--network` is passed
  to replay;
- the recorded sources are still available.

Replay rebuilds one platform at a time. For a multi-platform image, select the
platform with `--platform`. By default, the only platform of the image or the
default platform of the builder is used.

## Related

- [SLSA Provenance v1](https://slsa.dev/provenance/v1)
- [`docker buildx history`](buildx_history.md) — inspect locally recorded builds
