# docker buildx history import

```text
docker buildx history import [OPTIONS] -
```

<!---MARKER_GEN_START-->
Import build records into Docker Desktop

### Options

| Name                             | Type          | Default | Description                                                          |
|:---------------------------------|:--------------|:--------|:---------------------------------------------------------------------|
| `--builder`                      | `string`      |         | Override the configured builder instance                             |
| `-D`, `--debug`                  | `bool`        |         | Enable debug logging                                                 |
| [`-f`](#file), [`--file`](#file) | `stringArray` |         | Import from a file path                                              |
| `--timeout`                      | `duration`    | `20s`   | Override the default global timeout (as duration, for example 1m20s) |


<!---MARKER_GEN_END-->

## Description

Import a build record from a `.dockerbuild` archive into Docker Desktop. This
lets you view, inspect, and analyze builds created in other environments or CI
pipelines.

## Examples

### Import a `.dockerbuild` archive from standard input

```console
docker buildx history import < mybuild.dockerbuild
```

### <a name="file"></a> Import a build archive from a file (--file)

```console
docker buildx history import --file ./artifacts/backend-build.dockerbuild
```

### Open a build manually

By default, the `import` command automatically opens the imported build in Docker
Desktop. You don't need to run `open` unless you're opening a specific build
or re-opening it later.

If you've imported multiple builds, you can open one manually:

```console
docker buildx history open ci-build
```
