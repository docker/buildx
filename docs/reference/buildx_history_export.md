# docker buildx history export

<!---MARKER_GEN_START-->
Export a build into Docker Desktop bundle

### Options

| Name             | Type     | Default | Description                              |
|:-----------------|:---------|:--------|:-----------------------------------------|
| `--all`          | `bool`   |         | Export all records for the builder       |
| `--builder`      | `string` |         | Override the configured builder instance |
| `-D`, `--debug`  | `bool`   |         | Enable debug logging                     |
| `-o`, `--output` | `string` |         | Output file path                         |


<!---MARKER_GEN_END-->

## Description

Export one or more build records to `.dockerbuild` archive files. These archives
contain metadata, logs, and build outputs, and can be imported into Docker
Desktop or shared across environments.

## Examples

### Export a single build to a custom file

```console
docker buildx history export mybuild --output mybuild.dockerbuild
```

### Export multiple builds to individual `.dockerbuild` files

This example writes `mybuild.dockerbuild` and `backend-build.dockerbuild` to
the current directory:

```console
docker buildx history export mybuild backend-build
```

### Export all build records

```console
docker buildx history export --all
```
