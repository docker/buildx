# docker buildx debug build

<!---MARKER_GEN_START-->
Start a build

### Aliases

`docker build`, `docker builder build`, `docker image build`, `docker buildx b`

### Options

| Name                | Type          | Default   | Description                                                                                                  |
|:--------------------|:--------------|:----------|:-------------------------------------------------------------------------------------------------------------|
| `--add-host`        | `stringSlice` |           | Add a custom host-to-IP mapping (format: `host:ip`)                                                          |
| `--allow`           | `stringSlice` |           | Allow extra privileged entitlement (e.g., `network.host`, `security.insecure`)                               |
| `--annotation`      | `stringArray` |           | Add annotation to the image                                                                                  |
| `--attest`          | `stringArray` |           | Attestation parameters (format: `type=sbom,generator=image`)                                                 |
| `--build-arg`       | `stringArray` |           | Set build-time variables                                                                                     |
| `--build-context`   | `stringArray` |           | Additional build contexts (e.g., name=path)                                                                  |
| `--builder`         | `string`      |           | Override the configured builder instance                                                                     |
| `--cache-from`      | `stringArray` |           | External cache sources (e.g., `user/app:cache`, `type=local,src=path/to/dir`)                                |
| `--cache-to`        | `stringArray` |           | Cache export destinations (e.g., `user/app:cache`, `type=local,dest=path/to/dir`)                            |
| `--call`            | `string`      | `build`   | Set method for evaluating build (`check`, `outline`, `targets`)                                              |
| `--cgroup-parent`   | `string`      |           | Set the parent cgroup for the `RUN` instructions during build                                                |
| `--check`           | `bool`        |           | Shorthand for `--call=check`                                                                                 |
| `-D`, `--debug`     | `bool`        |           | Enable debug logging                                                                                         |
| `--detach`          | `bool`        |           | Detach buildx server (supported only on linux) (EXPERIMENTAL)                                                |
| `-f`, `--file`      | `string`      |           | Name of the Dockerfile (default: `PATH/Dockerfile`)                                                          |
| `--iidfile`         | `string`      |           | Write the image ID to a file                                                                                 |
| `--label`           | `stringArray` |           | Set metadata for an image                                                                                    |
| `--load`            | `bool`        |           | Shorthand for `--output=type=docker`                                                                         |
| `--metadata-file`   | `string`      |           | Write build result metadata to a file                                                                        |
| `--network`         | `string`      | `default` | Set the networking mode for the `RUN` instructions during build                                              |
| `--no-cache`        | `bool`        |           | Do not use cache when building the image                                                                     |
| `--no-cache-filter` | `stringArray` |           | Do not cache specified stages                                                                                |
| `-o`, `--output`    | `stringArray` |           | Output destination (format: `type=local,dest=path`)                                                          |
| `--platform`        | `stringArray` |           | Set target platform for build                                                                                |
| `--progress`        | `string`      | `auto`    | Set type of progress output (`auto`, `quiet`, `plain`, `tty`, `rawjson`). Use plain to show container output |
| `--provenance`      | `string`      |           | Shorthand for `--attest=type=provenance`                                                                     |
| `--pull`            | `bool`        |           | Always attempt to pull all referenced images                                                                 |
| `--push`            | `bool`        |           | Shorthand for `--output=type=registry`                                                                       |
| `-q`, `--quiet`     | `bool`        |           | Suppress the build output and print image ID on success                                                      |
| `--root`            | `string`      |           | Specify root directory of server to connect (EXPERIMENTAL)                                                   |
| `--sbom`            | `string`      |           | Shorthand for `--attest=type=sbom`                                                                           |
| `--secret`          | `stringArray` |           | Secret to expose to the build (format: `id=mysecret[,src=/local/secret]`)                                    |
| `--server-config`   | `string`      |           | Specify buildx server config file (used only when launching new server) (EXPERIMENTAL)                       |
| `--shm-size`        | `bytes`       | `0`       | Shared memory size for build containers                                                                      |
| `--ssh`             | `stringArray` |           | SSH agent socket or keys to expose to the build (format: `default\|<id>[=<socket>\|<key>[,<key>]]`)          |
| `-t`, `--tag`       | `stringArray` |           | Name and optionally a tag (format: `name:tag`)                                                               |
| `--target`          | `string`      |           | Set the target build stage to build                                                                          |
| `--ulimit`          | `ulimit`      |           | Ulimit options                                                                                               |


<!---MARKER_GEN_END-->

