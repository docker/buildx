# docker buildx history trace

<!---MARKER_GEN_START-->
Show the OpenTelemetry trace of a build record

### Options

| Name            | Type     | Default       | Description                              |
|:----------------|:---------|:--------------|:-----------------------------------------|
| `--addr`        | `string` | `127.0.0.1:0` | Address to bind the UI server            |
| `--builder`     | `string` |               | Override the configured builder instance |
| `--compare`     | `string` |               | Compare with another build reference     |
| `-D`, `--debug` | `bool`   |               | Enable debug logging                     |


<!---MARKER_GEN_END-->

## Description

View the OpenTelemetry trace for a completed build. This command loads the
trace into a Jaeger UI running in a local container
(or a custom instance if configured) and opens it in your browser.

This helps analyze build performance, step timing, and internal execution flows.

## Examples

### Open the OpenTelemetry trace for the most recent build

```console
docker buildx history trace
```

### Open the trace for a specific build

```console
docker buildx history trace mybuild
```

### Run the Jaeger UI on a specific port

```console
docker buildx history trace mybuild --addr 127.0.0.1:16686
```

### Compare two build traces

```console
docker buildx history trace --compare mybuild:main mybuild:feature
```
