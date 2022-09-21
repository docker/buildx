# Local exporter

The `local` exporter outputs the root filesystem of the build result into a
local directory. This exporter can be used when using buildkit to build
something other than container images. 

This exporter is often paired with [multi-stage]() builds, to export only a
minimal number of build artifacts, such as self-contained binaries.

## Synopsis

Build a container image using the `local` exporter:

```console
$ docker buildx build --output type=local[,parameters] .
```

The following table describes the available parameters that you can pass to
`--output` for `type=local`:

| Parameter | Value  | Default | Description           |
| --------- | ------ | ------- | --------------------- |
| `dest`    | String |         | Path to copy files to |

## Further reading
