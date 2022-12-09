# buildx ls

```
docker buildx ls
```

<!---MARKER_GEN_START-->
List builder instances

### Options

| Name | Type | Default | Description |
| --- | --- | --- | --- |
| [`--format`](#format) | `string` | `table` | Format the output |
| `--no-trunc` |  |  | Do not truncate output |
| [`-q`](#quiet), [`--quiet`](#quiet) |  |  | Only display builder names |


<!---MARKER_GEN_END-->

## Description

Lists all builder instances and the nodes for each instance.

```console
$ docker buildx ls
NAME/NODE           DRIVER/ENDPOINT                   STATUS    BUILDKIT   PLATFORMS
elated_tesla*       docker-container
 \_ elated_tesla0    \_ unix:///var/run/docker.sock   running   v0.10.3    linux/amd64
 \_ elated_tesla1    \_ ssh://ubuntu@1.2.3.4          running   v0.10.3    linux/arm64*, linux/arm/v7, linux/arm/v6
default             docker
 \_ default          \_ default                       running   20.10.14   linux/amd64
```

Each builder has one or more nodes associated with it. The current builder's
name is marked with a `*` in `NAME/NODE` and explicit node to build against for
the target platform marked with a `*` in the `PLATFORMS` column.

## Examples

### <a name="format"></a> Format the output (--format)

The formatting options (`--format`) pretty-prints tasks output using a Go
template.

Valid placeholders for the Go template are listed below:

| Placeholder       | Description                                 |
|-------------------|---------------------------------------------|
| `.NameNode`       | Name of the builder or node                 |
| `.DriverEndpoint` | Driver (for builder) or Endpoint (for node) |
| `.LastActivity`   | Builder last activity                       |
| `.Status`         | Node or builder status                      |
| `.Buildkit`       | BuildKit version of the node                |
| `.Platforms`      | Available node's platforms                  |
| `.Error`          | Error                                       |
| `.IsNode`         | `true` if item is a node                    |
| `.IsCurrent`      | `true` if item is the active builder        |

When using the `--format` option, the `ls` command will either output the data
exactly as the template declares or, when using the `table` directive, includes
column headers as well.

The following example uses a template without headers and outputs the
`NameNode` and `DriverEndpoint` entries separated by a colon (`:`):

```console
$ docker buildx ls --format "{{.NameNode}}: {{.DriverEndpoint}}"
elated_tesla: docker-container
elated_tesla0: unix:///var/run/docker.sock
elated_tesla1: ssh://ubuntu@1.2.3.4
default: docker
default: default
```

### <a name="quiet"></a> Only display builder names (-q, --quiet)

The `-q` or `--quiet` option only shows names of the builders:

```console
$ docker buildx ls -q
elated_tesla
default
```
