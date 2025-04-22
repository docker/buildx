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

### <a name="inspect-provenance-attachment"></a> Inspect a provenance attachment from a build

Supported types include `provenance` and `sbom`.

```console
$ docker buildx history inspect attachment qu2gsuo8ejqrwdfii23xkkckt --type provenance
{
  "_type": "https://slsa.dev/provenance/v0.2",
  "buildDefinition": {
    "buildType": "https://build.docker.com/BuildKit@v1",
    "externalParameters": {
      "target": "app",
      "platforms": ["linux/amd64"]
    }
  },
  "runDetails": {
    "builder": "docker",
    "by": "ci@docker.com"
  }
}
```

### <a name="insepct-SBOM"></a> Inspect a SBOM for linux/amd64

```console
$ docker buildx history inspect attachment ^0 \
  --type sbom \
  --platform linux/amd64
{
  "bomFormat": "CycloneDX",
  "specVersion": "1.5",
  "version": 1,
  "components": [
    {
      "type": "library",
      "name": "alpine",
      "version": "3.18.2"
    }
  ]
}
```

### <a name="inspect-attachment-digest"></a> Inspect an attachment by digest

You can inspect an attachment directly using its digset, which you can get from
the `inspect` output:

```console
# Using a build ID
docker buildx history inspect attachment qu2gsuo8ejqrwdfii23xkkckt sha256:abcdef123456...

# Or using a relative offset
docker buildx history inspect attachment ^0 sha256:abcdef123456...
```

Use `--type sbom` or `--type provenance` to filter attachments by type. To
inspect a specific attachment by digest, omit the `--type` flag.
