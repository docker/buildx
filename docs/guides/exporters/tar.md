# Tarball exporter

The `tar` exporter outputs the root filesystem from the build result into a
local tarball file. This exporter operates similarly to the [`local` exporter](local.md),
however, instead of exporting multiple files together, it bundles all of them
up together into a POSIX tar.

## Synopsis

Build a container image using the `tar` exporter:

```console
$ docker buildx build --output type=tar[,parameters] .
```

The following table describes the available parameters that you can pass to
`--output` for `type=tar`:

| Parameter | Value  | Default | Description                     |
| --------- | ------ | ------- | ------------------------------- |
| `dest`    | String |         | Path to generate the tarball at |

## Further reading
