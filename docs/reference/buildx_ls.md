# buildx ls

```text
docker buildx ls
```

<!---MARKER_GEN_START-->
List builder instances

### Options

| Name                  | Type     | Default | Description           |
|:----------------------|:---------|:--------|:----------------------|
| `-D`, `--debug`       | `bool`   |         | Enable debug logging  |
| [`--format`](#format) | `string` | `table` | Format the output     |
| `--no-trunc`          | `bool`   |         | Don't truncate output |


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
 \_ default          \_ default                       running   v0.8.2     linux/amd64
```

Each builder has one or more nodes associated with it. The current builder's
name is marked with a `*` in `NAME/NODE` and explicit node to build against for
the target platform marked with a `*` in the `PLATFORMS` column.

## Examples

### <a name="format"></a> Format the output (--format)

The formatting options (`--format`) pretty-prints builder instances output
using a Go template.

Valid placeholders for the Go template are listed below:

| Placeholder       | Description                                 |
|-------------------|---------------------------------------------|
| `.Name`           | Builder or node name                        |
| `.DriverEndpoint` | Driver (for builder) or Endpoint (for node) |
| `.LastActivity`   | Builder last activity                       |
| `.Status`         | Builder or node status                      |
| `.Buildkit`       | BuildKit version of the node                |
| `.Platforms`      | Available node's platforms                  |
| `.Error`          | Error                                       |
| `.Builder`        | Builder object                              |

When using the `--format` option, the `ls` command will either output the data
exactly as the template declares or, when using the `table` directive, includes
column headers as well.

The following example uses a template without headers and outputs the
`Name` and `DriverEndpoint` entries separated by a colon (`:`):

```console
$ docker buildx ls --format "{{.Name}}: {{.DriverEndpoint}}"
elated_tesla: docker-container
elated_tesla0: unix:///var/run/docker.sock
elated_tesla1: ssh://ubuntu@1.2.3.4
default: docker
default: default
```

The `Builder` placeholder can be used to access the builder object and its
fields. For example, the following template outputs the builder's and
nodes' names with their respective endpoints:

```console
$ docker buildx ls --format "{{.Builder.Name}}: {{range .Builder.Nodes}}\n  {{.Name}}: {{.Endpoint}}{{end}}"
elated_tesla:
  elated_tesla0: unix:///var/run/docker.sock
  elated_tesla1: ssh://ubuntu@1.2.3.4
default: docker
  default: default
```
