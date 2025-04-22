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

### <a name="export-single-build"></a> Export a single build to a custom file

```console
docker buildx history export qu2gsuo8ejqrwdfii23xkkckt --output mybuild.dockerbuild
```

You can find build IDs by running:

```console
docker buildx history ls
```

### <a name="export-multiple-builds"></a> Export multiple builds to individual `.dockerbuild` files

To export two builds to separate files:

```console
# Using build IDs
docker buildx history export qu2gsuo8ejqrwdfii23xkkckt -o mybuild.dockerbuild
docker buildx history export qsiifiuf1ad9pa9qvppc0z1l3 -o backend-build.dockerbuild

# Or using relative offsets
docker buildx history export ^1 -o mybuild.dockerbuild
docker buildx history export ^2 -o backend-build.dockerbuild
```

Or use shell redirection:

```console
docker buildx history export ^1 > mybuild.dockerbuild
docker buildx history export ^2 > backend-build.dockerbuild
```

### <a name="export-all-builds"></a> Export all build records to a file

Use the `--all` flag and redirect the output:

```console
docker buildx history export --all > all-builds.dockerbuild
```

Or use the `--output` flag:

```console
docker buildx history export --all -o all-builds.dockerbuild
```
