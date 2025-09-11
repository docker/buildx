# buildx bake

```text
docker buildx bake [OPTIONS] [TARGET...]
```

<!---MARKER_GEN_START-->
Build from a file

### Aliases

`docker buildx bake`, `docker buildx f`

### Options

| Name                                | Type          | Default | Description                                                                                                  |
|:------------------------------------|:--------------|:--------|:-------------------------------------------------------------------------------------------------------------|
| [`--allow`](#allow)                 | `stringArray` |         | Allow build to access specified resources                                                                    |
| [`--builder`](#builder)             | `string`      |         | Override the configured builder instance                                                                     |
| [`--call`](#call)                   | `string`      | `build` | Set method for evaluating build (`check`, `outline`, `targets`)                                              |
| [`--check`](#check)                 | `bool`        |         | Shorthand for `--call=check`                                                                                 |
| `-D`, `--debug`                     | `bool`        |         | Enable debug logging                                                                                         |
| [`-f`](#file), [`--file`](#file)    | `stringArray` |         | Build definition file                                                                                        |
| [`--list`](#list)                   | `string`      |         | List targets or variables                                                                                    |
| [`--load`](#load)                   | `bool`        |         | Shorthand for `--set=*.output=type=docker`. Conditional.                                                     |
| [`--metadata-file`](#metadata-file) | `string`      |         | Write build result metadata to a file                                                                        |
| [`--no-cache`](#no-cache)           | `bool`        |         | Do not use cache when building the image                                                                     |
| [`--print`](#print)                 | `bool`        |         | Print the options without building                                                                           |
| [`--progress`](#progress)           | `string`      | `auto`  | Set type of progress output (`auto`, `quiet`, `plain`, `tty`, `rawjson`). Use plain to show container output |
| [`--provenance`](#provenance)       | `string`      |         | Shorthand for `--set=*.attest=type=provenance`                                                               |
| [`--pull`](#pull)                   | `bool`        |         | Always attempt to pull all referenced images                                                                 |
| [`--push`](#push)                   | `bool`        |         | Shorthand for `--set=*.output=type=registry`. Conditional.                                                   |
| [`--sbom`](#sbom)                   | `string`      |         | Shorthand for `--set=*.attest=type=sbom`                                                                     |
| [`--set`](#set)                     | `stringArray` |         | Override target value (e.g., `targetpattern.key=value`)                                                      |


<!---MARKER_GEN_END-->

## Description

Bake is a high-level build command. Each specified target runs in parallel
as part of the build.

Read [High-level build options with Bake](https://docs.docker.com/build/bake/)
guide for introduction to writing bake files.

> [!NOTE]
> `buildx bake` command may receive backwards incompatible features in the future
> if needed. We are looking for feedback on improving the command and extending
> the functionality further.

## Examples

### <a name="allow"></a> Allow extra privileged entitlement (--allow)

```text
--allow=ENTITLEMENT[=VALUE]
```

Entitlements are designed to provide controlled access to privileged
operations. By default, Buildx and BuildKit operates with restricted
permissions to protect users and their systems from unintended side effects or
security risks. The `--allow` flag explicitly grants access to additional
entitlements, making it clear when a build or bake operation requires elevated
privileges.

In addition to BuildKit's `network.host` and `security.insecure` entitlements
(see [`docker buildx build --allow`](https://docs.docker.com/reference/cli/docker/buildx/build/#allow),
Bake supports file system entitlements that grant granular control over file
system access. These are particularly useful when working with builds that need
access to files outside the default working directory.

Bake supports the following filesystem entitlements:

- `--allow fs=<path|*>` - Grant read and write access to files outside of the
  working directory.
- `--allow fs.read=<path|*>` - Grant read access to files outside of the
  working directory.
- `--allow fs.write=<path|*>` - Grant write access to files outside of the
  working directory.

The `fs` entitlements take a path value (relative or absolute) to a directory
on the filesystem. Alternatively, you can pass a wildcard (`*`) to allow Bake
to access the entire filesystem.

### Example: fs.read

Given the following Bake configuration, Bake would need to access the parent
directory, relative to the Bake file.

```hcl
target "app" {
  context = "../src"
}
```

Assuming `docker buildx bake app` is executed in the same directory as the
`docker-bake.hcl` file, you would need to explicitly allow Bake to read from
the `../src` directory. In this case, the following invocations all work:

```console
$ docker buildx bake --allow fs.read=* app
$ docker buildx bake --allow fs.read=../src app
$ docker buildx bake --allow fs=* app
```

### Example: fs.write

The following `docker-bake.hcl` file requires write access to the `/tmp`
directory.

```hcl
target "app" {
  output = "/tmp"
}
```

Assuming `docker buildx bake app` is executed outside of the `/tmp` directory,
you would need to allow the `fs.write` entitlement, either by specifying the
path or using a wildcard:

```console
$ docker buildx bake --allow fs=/tmp app
$ docker buildx bake --allow fs.write=/tmp app
$ docker buildx bake --allow fs.write=* app
```

### <a name="builder"></a> Override the configured builder instance (--builder)

Same as [`buildx --builder`](buildx.md#builder).

### <a name="call"></a> Invoke a frontend method (--call)

Same as [`build --call`](buildx_build.md#call).

#### <a name="check"></a> Call: check (--check)

Same as [`build --check`](buildx_build.md#check).

### <a name="file"></a> Specify a build definition file (-f, --file)

Use the `-f` / `--file` option to specify the build definition file to use.
The file can be an HCL, JSON or Compose file. If multiple files are specified,
all are read and the build configurations are combined.

Alternatively, the environment variable `BUILDX_BAKE_FILE` can be used to specify the build definition to use.
This is mutually exclusive with `-f` / `--file`; if both are specified, the environment variable is ignored.
Multiple definitions can be specified by separating them with the system's path separator
(typically `;` on Windows and `:` elsewhere), but can be changed with `BUILDX_BAKE_PATH_SEPARATOR`.

You can pass the names of the targets to build, to build only specific target(s).
The following example builds the `db` and `webapp-release` targets that are
defined in the `docker-bake.dev.hcl` file:

```hcl
# docker-bake.dev.hcl
group "default" {
  targets = ["db", "webapp-dev"]
}

target "webapp-dev" {
  dockerfile = "Dockerfile.webapp"
  tags = ["docker.io/username/webapp"]
}

target "webapp-release" {
  inherits = ["webapp-dev"]
  platforms = ["linux/amd64", "linux/arm64"]
}

target "db" {
  dockerfile = "Dockerfile.db"
  tags = ["docker.io/username/db"]
}
```

```console
$ docker buildx bake -f docker-bake.dev.hcl db webapp-release
```

See the [Bake file reference](https://docs.docker.com/build/bake/reference/)
for more details.

### <a name="list"></a> List targets and variables (--list)

The `--list` flag displays all available targets or variables in the Bake
configuration, along with a description (if set using the `description`
property in the Bake file).

To list all targets:

```console {title="List targets"}
$ docker buildx bake --list=targets
TARGET              DESCRIPTION
binaries
default             binaries
update-docs
validate
validate-golangci   Validate .golangci.yml schema (does not run Go linter)
```

To list variables:

```console
$ docker buildx bake --list=variables
VARIABLE      TYPE      VALUE                DESCRIPTION
REGISTRY      string    docker.io/username   Registry and namespace
IMAGE_NAME    string    my-app               Image name
GO_VERSION              <null>
DEBUG         bool      false                Add debug symbols
```

Variable types will be shown when set using the `type` property in the Bake file.

By default, the output of `docker buildx bake --list` is presented in a table
format. Alternatively, you can use a long-form CSV syntax and specify a
`format` attribute to output the list in JSON.

```console
$ docker buildx bake --list=type=targets,format=json
```

### <a name="load"></a> Load images into Docker (--load)

The `--load` flag is a convenience shorthand for adding an image export of type 
`docker`:

```console
--load   ≈   --set=*.output=type=docker
```

However, its behavior is conditional:

- If the build definition has no output defined, `--load` adds
`type=docker`.
- If the build definition’s outputs are `docker`, `image`, `registry`,
`oci`, `--load` will add a `type=docker` export if one is not already present.
- If the build definition contains `local` or `tar` outputs,
`--load` does nothing. It will not override those outputs.

For example, with the following bake file:

```hcl
target "default" {
  output = ["type=tar,dest=hi.tar"]
}
```

With `--load`:

```console
$ docker buildx bake --load --print
...
"output": [
  { 
    "dest": "hi.tar"
    "type": "tar",
   }
]
```

The `tar` output remains unchanged.

### <a name="metadata-file"></a> Write build results metadata to a file (--metadata-file)

Similar to [`buildx build --metadata-file`](buildx_build.md#metadata-file) but
writes a map of results for each target such as:

```hcl
# docker-bake.hcl
group "default" {
  targets = ["db", "webapp-dev"]
}

target "db" {
  dockerfile = "Dockerfile.db"
  tags = ["docker.io/username/db"]
}

target "webapp-dev" {
  dockerfile = "Dockerfile.webapp"
  tags = ["docker.io/username/webapp"]
}
```

```console
$ docker buildx bake --load --metadata-file metadata.json .
$ cat metadata.json
```

```json
{
  "buildx.build.warnings": {},
  "db": {
    "buildx.build.provenance": {},
    "buildx.build.ref": "mybuilder/mybuilder0/0fjb6ubs52xx3vygf6fgdl611",
    "containerimage.config.digest": "sha256:2937f66a9722f7f4a2df583de2f8cb97fc9196059a410e7f00072fc918930e66",
    "containerimage.descriptor": {
      "annotations": {
        "config.digest": "sha256:2937f66a9722f7f4a2df583de2f8cb97fc9196059a410e7f00072fc918930e66",
        "org.opencontainers.image.created": "2022-02-08T21:28:03Z"
      },
      "digest": "sha256:19ffeab6f8bc9293ac2c3fdf94ebe28396254c993aea0b5a542cfb02e0883fa3",
      "mediaType": "application/vnd.oci.image.manifest.v1+json",
      "size": 506
    },
    "containerimage.digest": "sha256:19ffeab6f8bc9293ac2c3fdf94ebe28396254c993aea0b5a542cfb02e0883fa3"
  },
  "webapp-dev": {
    "buildx.build.provenance": {},
    "buildx.build.ref": "mybuilder/mybuilder0/kamngmcgyzebqxwu98b4lfv3n",
    "containerimage.config.digest": "sha256:9651cc2b3c508f697c9c43b67b64c8359c2865c019e680aac1c11f4b875b67e0",
    "containerimage.descriptor": {
      "annotations": {
        "config.digest": "sha256:9651cc2b3c508f697c9c43b67b64c8359c2865c019e680aac1c11f4b875b67e0",
        "org.opencontainers.image.created": "2022-02-08T21:28:15Z"
      },
      "digest": "sha256:6d9ac9237a84afe1516540f40a0fafdc86859b2141954b4d643af7066d598b74",
      "mediaType": "application/vnd.oci.image.manifest.v1+json",
      "size": 506
    },
    "containerimage.digest": "sha256:6d9ac9237a84afe1516540f40a0fafdc86859b2141954b4d643af7066d598b74"
  }
}
```

> [!NOTE]
> Build record [provenance](https://docs.docker.com/build/metadata/attestations/slsa-provenance/#provenance-attestation-example)
> (`buildx.build.provenance`) includes minimal provenance by default. Set the
> `BUILDX_METADATA_PROVENANCE` environment variable to customize this behavior:
> * `min` sets minimal provenance (default).
> * `max` sets full provenance.
> * `disabled`, `false` or `0` does not set any provenance.

> [!NOTE]
> Build warnings (`buildx.build.warnings`) are not included by default. Set the
> `BUILDX_METADATA_WARNINGS` environment variable to `1` or `true` to
> include them.

### <a name="no-cache"></a> Don't use cache when building the image (--no-cache)

Same as `build --no-cache`. Don't use cache when building the image.

### <a name="print"></a> Print the options without building (--print)

Prints the resulting options of the targets desired to be built, in a JSON
format, without starting a build.

```console
$ docker buildx bake -f docker-bake.hcl --print db
{
  "group": {
    "default": {
      "targets": [
        "db"
      ]
    }
  },
  "target": {
    "db": {
      "context": "./",
      "dockerfile": "Dockerfile",
      "tags": [
        "docker.io/tiborvass/db"
      ]
    }
  }
}
```

### <a name="progress"></a> Set type of progress output (--progress)

Same as [`build --progress`](buildx_build.md#progress).

### <a name="provenance"></a> Create provenance attestations (--provenance)

Same as [`build --provenance`](buildx_build.md#provenance).

### <a name="pull"></a> Always attempt to pull a newer version of the image (--pull)

Same as `build --pull`.

### <a name="push"></a> Push images to a registry (--push)

The `--push` flag follows the same logic as `--load`:

- If no outputs are defined, it adds a `type=image,push=true` export.
- For existing `image` outputs, it sets `push=true`.
- If outputs are set to `local` or `tar`, it does not override them.

### <a name="sbom"></a> Create SBOM attestations (--sbom)

Same as [`build --sbom`](buildx_build.md#sbom).

### <a name="set"></a> Override target configurations from command line (--set)

```
--set targetpattern.key[.subkey]=value
```

Override target configurations from command line. The pattern matching syntax
is defined in https://golang.org/pkg/path/#Match.

```console
$ docker buildx bake --set target.args.mybuildarg=value
$ docker buildx bake --set target.platform=linux/arm64
$ docker buildx bake --set foo*.args.mybuildarg=value   # overrides build arg for all targets starting with 'foo'
$ docker buildx bake --set *.platform=linux/arm64       # overrides platform for all targets
$ docker buildx bake --set foo*.no-cache                # bypass caching only for targets starting with 'foo'
$ docker buildx bake --set target.platform+=linux/arm64 # appends 'linux/arm64' to the platform list
```

You can override the following fields:

* `annotations`
* `attest`
* `args`
* `cache-from`
* `cache-to`
* `call`
* `context`
* `dockerfile`
* `entitlements`
* `extra-hosts`
* `labels`
* `load`
* `no-cache`
* `no-cache-filter`
* `output`
* `platform`
* `pull`
* `push`
* `secrets`
* `ssh`
* `tags`
* `target`

You can append using `+=` operator for the following fields:

* `annotations`¹
* `attest`¹
* `cache-from`
* `cache-to`
* `entitlements`¹
* `no-cache-filter`
* `output`
* `platform`
* `secrets`
* `ssh`
* `tags`

> [!NOTE]
> ¹ These fields already append by default.
