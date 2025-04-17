# docker buildx history open

<!---MARKER_GEN_START-->
Open a build in Docker Desktop

### Options

| Name            | Type     | Default | Description                              |
|:----------------|:---------|:--------|:-----------------------------------------|
| `--builder`     | `string` |         | Override the configured builder instance |
| `-D`, `--debug` | `bool`   |         | Enable debug logging                     |


<!---MARKER_GEN_END-->

## Description

Open a build record in Docker Desktop for visual inspection. This requires Docker Desktop to be installed and running on the host machine.

## Examples

### Open the most recent build in Docker Desktop

```console
docker buildx history open
```

### Open a specific build by name

```console
docker buildx history open mybuild
```
