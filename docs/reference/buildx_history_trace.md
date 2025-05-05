# docker buildx history trace

<!---MARKER_GEN_START-->
Show the OpenTelemetry trace of a build record

### Options

| Name                    | Type     | Default       | Description                              |
|:------------------------|:---------|:--------------|:-----------------------------------------|
| [`--addr`](#addr)       | `string` | `127.0.0.1:0` | Address to bind the UI server            |
| `--builder`             | `string` |               | Override the configured builder instance |
| [`--compare`](#compare) | `string` |               | Compare with another build reference     |
| `-D`, `--debug`         | `bool`   |               | Enable debug logging                     |


<!---MARKER_GEN_END-->

## Description

View the OpenTelemetry trace for a completed build. This command loads the
trace into a Jaeger UI viewer and opens it in your browser.

This helps analyze build performance, step timing, and internal execution flows.

## Examples

### Open the OpenTelemetry trace for the most recent build

This command starts a temporary Jaeger UI server and opens your default browser
to view the trace.

```console
docker buildx history trace
```

### Open the trace for a specific build

```console
# Using a build ID
docker buildx history trace qu2gsuo8ejqrwdfii23xkkckt

# Or using a relative offset
docker buildx history trace ^1
```

### <a name="addr"></a> Run the Jaeger UI on a specific port (--addr)

```console
# Using a build ID
docker buildx history trace qu2gsuo8ejqrwdfii23xkkckt --addr 127.0.0.1:16686

# Or using a relative offset
docker buildx history trace ^1 --addr 127.0.0.1:16686
```

### <a name="compare"></a> Compare two build traces (--compare)

Compare two specific builds by name:

```console
# Using build IDs
docker buildx history trace --compare=qu2gsuo8ejqrwdfii23xkkckt qsiifiuf1ad9pa9qvppc0z1l3

# Or using a single relative offset
docker buildx history trace --compare=^1
```

When you use a single reference with `--compare`, it compares that build
against the most recent one.
