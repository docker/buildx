# docker buildx replay

```text
docker buildx replay [OPTIONS] COMMAND
```

<!---MARKER_GEN_START-->
Replay a build from its provenance

### Subcommands

| Name                                    | Description                                                           |
|:----------------------------------------|:----------------------------------------------------------------------|
| [`build`](buildx_replay_build.md)       | Rebuild an image from provenance and pinned materials                 |
| [`snapshot`](buildx_replay_snapshot.md) | Export replay inputs for a subject as a reusable materials store      |
| [`verify`](buildx_replay_verify.md)     | Replay a subject and compare the result against the original artifact |


### Options

| Name            | Type     | Default | Description                              |
|:----------------|:---------|:--------|:-----------------------------------------|
| `--builder`     | `string` |         | Override the configured builder instance |
| `-D`, `--debug` | `bool`   |         | Enable debug logging                     |


<!---MARKER_GEN_END-->

## Description

`buildx replay` consumes a build's SLSA v1 provenance attestation and
reproduces the build with the recorded frontend, attrs, and material pins.
The feature is entirely client-side: it works against any BuildKit daemon
that supports the session source-policy capability.

Subjects are accepted in three forms:

- `docker-image://<ref>` or a bare `<ref>` — resolve through the registry.
- `oci-layout://<path>[:<tag>]` — read from a local OCI layout.
- A local in-toto attestation file (`.intoto.jsonl` or a DSSE envelope).

Multi-platform inputs expand into N subjects, one per child manifest. Each
subject is replayed independently.

## Related

- [SLSA Provenance v1](https://slsa.dev/provenance/v1)
- [`docker buildx history`](buildx_history.md) — inspect locally recorded builds
