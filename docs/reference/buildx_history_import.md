# docker buildx history import

<!---MARKER_GEN_START-->
Import a build into Docker Desktop

### Options

| Name            | Type          | Default | Description                              |
|:----------------|:--------------|:--------|:-----------------------------------------|
| `--builder`     | `string`      |         | Override the configured builder instance |
| `-D`, `--debug` | `bool`        |         | Enable debug logging                     |
| `-f`, `--file`  | `stringArray` |         | Import from a file path                  |


<!---MARKER_GEN_END-->

## Description

Import a build record from a `.dockerbuild` archive into Docker Desktop. This
lets you view, inspect, and analyze builds created in other environments or CI
pipelines.

## Examples

### Import a `.dockerbuild` archive into Docker Desktop

```console
docker buildx history import < mybuild.dockerbuild
```

### Import a file using a specific path

```console
docker buildx history import --file ./artifacts/backend-build.dockerbuild
```

### Import a file and open it in Docker Desktop

```console
docker buildx history import --file ./ci-build.dockerbuild && docker buildx history open ci-build
```
