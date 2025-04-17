# docker buildx history inspect attachment

<!---MARKER_GEN_START-->
Inspect a build attachment

### Options

| Name            | Type     | Default | Description                              |
|:----------------|:---------|:--------|:-----------------------------------------|
| `--builder`     | `string` |         | Override the configured builder instance |
| `-D`, `--debug` | `bool`   |         | Enable debug logging                     |
| `--platform`    | `string` |         | Platform of attachment                   |
| `--type`        | `string` |         | Type of attachment                       |


<!---MARKER_GEN_END-->

## Description

Inspect a specific attachment from a build record, such as a provenance file or
SBOM. Attachments are optional artifacts stored with the build and may be
platform-specific.

## Examples

### Inspect a provenance attachment from a build

```console
docker buildx history inspect attachment mybuild --type https://slsa.dev/provenance/v0.2
```

### Inspect a SBOM for linux/amd64

```console
docker buildx history inspect attachment mybuild \
  --type application/vnd.cyclonedx+json \
  --platform linux/amd64
```
