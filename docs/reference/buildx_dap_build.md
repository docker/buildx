# docker buildx dap build

<!---MARKER_GEN_START-->
Start a build

### Options

| Name                | Type          | Default   | Description                                                                                                  |
|:--------------------|:--------------|:----------|:-------------------------------------------------------------------------------------------------------------|
| `--add-host`        | `stringSlice` |           | Add a custom host-to-IP mapping (format: `host:ip`)                                                          |
| `--allow`           | `stringArray` |           | Allow extra privileged entitlement (e.g., `network.host`, `security.insecure`)                               |
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
| `--sbom`            | `string`      |           | Shorthand for `--attest=type=sbom`                                                                           |
| `--secret`          | `stringArray` |           | Secret to expose to the build (format: `id=mysecret[,src=/local/secret]`)                                    |
| `--shm-size`        | `bytes`       | `0`       | Shared memory size for build containers                                                                      |
| `--ssh`             | `stringArray` |           | SSH agent socket or keys to expose to the build (format: `default\|<id>[=<socket>\|<key>[,<key>]]`)          |
| `-t`, `--tag`       | `stringArray` |           | Name and optionally a tag (format: `name:tag`)                                                               |
| `--target`          | `string`      |           | Set the target build stage to build                                                                          |
| `--ulimit`          | `ulimit`      |           | Ulimit options                                                                                               |


<!---MARKER_GEN_END-->

## Description

Start a debug session using the [debug adapter protocol](https://microsoft.github.io/debug-adapter-protocol/overview) to communicate with the debugger UI.

Arguments are the same as the `build`

> [!NOTE]
> `buildx dap build` command may receive backwards incompatible features in the future
> if needed. We are looking for feedback on improving the command and extending
> the functionality further.

## Examples

### <a name="launch-config"></a> Launch request arguments

The following [launch request arguments](https://microsoft.github.io/debug-adapter-protocol/specification#Requests_Launch) are supported. These are sent as a JSON body as part of the launch request.

| Name                | Type          | Default      | Description                                                                  |
|:--------------------|:--------------|:-------------|:-----------------------------------------------------------------------------|
| `dockerfile`        | `string`      | `Dockerfile` | Name of the Dockerfile                                                       |
| `contextPath`       | `string`      | `.`          | Set the context path for the build (normally the first positional argument)  |
| `target`            | `string`      |              | Set the target build stage to build                                          |
| `stopOnEntry`       | `boolean`     | `false`      | Stop on the first instruction                                                |

### <a name="additional-args"></a> Additional Arguments

Command line arguments may be passed to the debug adapter the same way they would be passed to the normal build command and they will set the value.
Launch request arguments that are set will override command line arguments if they are present.

A debug extension should include an `args` entry in the launch configuration and should append these arguments to the end of the tool invocation.
For example, a launch configuration in Visual Studio Code with the following:

```json
{
    "args": ["--build-arg", "FOO=AAA"]
}
```

This should cause the debug adapter to be invoked as `docker buildx dap build --build-arg FOO=AAA`.
