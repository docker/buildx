# docker buildx history ls

<!---MARKER_GEN_START-->
List build records

### Options

| Name            | Type          | Default | Description                                  |
|:----------------|:--------------|:--------|:---------------------------------------------|
| `--builder`     | `string`      |         | Override the configured builder instance     |
| `-D`, `--debug` | `bool`        |         | Enable debug logging                         |
| `--filter`      | `stringArray` |         | Provide filter values (e.g., `status=error`) |
| `--format`      | `string`      | `table` | Format the output                            |
| `--local`       | `bool`        |         | List records for current repository only     |
| `--no-trunc`    | `bool`        |         | Don't truncate output                        |


<!---MARKER_GEN_END-->

## Description

List completed builds recorded by the active builder. Each entry includes the
build ID, name (if available), status, timestamp, and duration.

By default, only records for the current builder are shown. You can filter
results using flags.

## Examples

### List all build records for the current builder

```console
docker buildx history ls
```

### List only failed builds

```console
docker buildx history ls --filter status=error
```

### List builds from the current directory

```console
docker buildx history ls --local
```

### Display full output without truncation

```console
docker buildx history ls --no-trunc
```

### Format output as JSON

```console
docker buildx history ls --format json
```

### Use a Go template to print name and durations

```console
docker buildx history ls --format '{{.Name}} - {{.Duration}}'
```
