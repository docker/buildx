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

### <a name="import-dockerbuild"></a> Import a `.dockerbuild` archive from standard input

```console
docker buildx history import < mybuild.dockerbuild
```

### <a name="import-build-archive"></a> Import a build archive from a file

```console
docker buildx history import --file ./artifacts/backend-build.dockerbuild
```

### <a name="open-build-manually"></a> Open a build manually

By default, the `import` command automatically opens the imported build in Docker
Desktop. You don't need to run `open` unless you're opening a specific build
or re-opening it later.

If you've imported multiple builds, you can open one manually:

```console
docker buildx history open ci-build
```
