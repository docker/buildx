# docker buildx history ls

<!---MARKER_GEN_START-->
List build records

### Options

| Name                      | Type          | Default | Description                                  |
|:--------------------------|:--------------|:--------|:---------------------------------------------|
| `--builder`               | `string`      |         | Override the configured builder instance     |
| `-D`, `--debug`           | `bool`        |         | Enable debug logging                         |
| [`--filter`](#filter)     | `stringArray` |         | Provide filter values (e.g., `status=error`) |
| [`--format`](#format)     | `string`      | `table` | Format the output                            |
| [`--local`](#local)       | `bool`        |         | List records for current repository only     |
| [`--no-trunc`](#no-trunc) | `bool`        |         | Don't truncate output                        |


<!---MARKER_GEN_END-->

## Description

List completed builds recorded by the active builder. Each entry includes the
build ID, name, status, timestamp, and duration.

By default, only records for the current builder are shown. You can filter
results using flags.

## Examples

### List all build records for the current builder

```console
$ docker buildx history ls
BUILD ID                    NAME           STATUS     CREATED AT        DURATION
qu2gsuo8ejqrwdfii23xkkckt   .dev/2850      Completed  3 days ago        1.4s
qsiifiuf1ad9pa9qvppc0z1l3   .dev/2850      Completed  3 days ago        1.3s
g9808bwrjrlkbhdamxklx660b   .dev/3120      Completed  5 days ago        2.1s
```

### <a name="filter"></a> List failed builds (--filter)

```console
docker buildx history ls --filter status=error
```

You can filter the list using the `--filter` flag. Supported filters include:

| Filter | Supported comparisons | Example |
|:-------|:----------------------|:--------|
| `ref`, `repository`, `status` | Support `=` and `!=` comparisons | `--filter status!=success` |
| `startedAt`, `completedAt`, `duration` | Support `<` and `>` comparisons with time values | `--filter duration>30s` |

You can combine multiple filters by repeating the `--filter` flag:

```console
docker buildx history ls --filter status=error --filter duration>30s
```

### <a name="local"></a> List builds from the current project (--local)

```console
docker buildx history ls --local
```

### <a name="no-trunc"></a> Display full output without truncation (--no-trunc)

```console
docker buildx history ls --no-trunc
```

### <a name="format"></a> Format output (--format)

**JSON output**

```console
$ docker buildx history ls --format json
[
  {
    "ID": "qu2gsuo8ejqrwdfii23xkkckt",
    "Name": ".dev/2850",
    "Status": "Completed",
    "CreatedAt": "2025-04-15T12:33:00Z",
    "Duration": "1.4s"
  },
  {
    "ID": "qsiifiuf1ad9pa9qvppc0z1l3",
    "Name": ".dev/2850",
    "Status": "Completed",
    "CreatedAt": "2025-04-15T12:29:00Z",
    "Duration": "1.3s"
  }
]
```

**Go template output**

```console
$ docker buildx history ls --format '{{.Name}} - {{.Duration}}'
.dev/2850 - 1.4s
.dev/2850 - 1.3s
.dev/3120 - 2.1s
```
