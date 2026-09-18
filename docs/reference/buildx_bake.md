# buildx bake

```text
docker buildx bake [OPTIONS] [TARGET...]
```

<!---MARKER_GEN_START-->
Build from a file

### Aliases

`docker buildx bake`, `docker buildx f`

### Options

| Name                                | Type          | Default | Description                                                                                                           |
|:------------------------------------|:--------------|:--------|:----------------------------------------------------------------------------------------------------------------------|
| [`--allow`](#allow)                 | `stringArray` |         | Allow build to access specified resources                                                                             |
| [`--builder`](#builder)             | `string`      |         | Override the configured builder instance                                                                              |
| [`--call`](#call)                   | `string`      | `build` | Set method for evaluating build (`check`, `outline`, `targets`)                                                       |
| [`--check`](#check)                 | `bool`        |         | Shorthand for `--call=check`                                                                                          |
| `-D`, `--debug`                     | `bool`        |         | Enable debug logging                                                                                                  |
| [`-f`](#file), [`--file`](#file)    | `stringArray` |         | Build definition file                                                                                                 |
| [`--list`](#list)                   | `string`      |         | List targets or variables                                                                                             |
| [`--load`](#load)                   | `bool`        |         | Shorthand for `--set=*.output=type=docker`. Conditional.                                                              |
| [`--metadata-file`](#metadata-file) | `string`      |         | Write build result metadata to a file                                                                                 |
| [`--no-cache`](#no-cache)           | `bool`        |         | Do not use cache when building the image                                                                              |
| `--policy`                          | `stringArray` |         | Global policy evaluation options (format: `[disabled=true\|false][,strict=true\|false][,log-level=level]`)            |
| [`--print`](#print)                 | `bool`        |         | Print the options without building                                                                                    |
| [`--progress`](#progress)           | `string`      | `auto`  | Set type of progress output (`auto`, `none`,  `plain`, `quiet`, `rawjson`, `tty`). Use plain to show container output |
| [`--provenance`](#provenance)       | `string`      |         | Shorthand for `--set=*.attest=type=provenance`                                                                        |
| [`--pull`](#pull)                   | `bool`        |         | Always attempt to pull all referenced images                                                                          |
| [`--push`](#push)                   | `bool`        |         | Shorthand for `--set=*.output=type=registry`. Conditional.                                                            |
| [`--sbom`](#sbom)                   | `string`      |         | Shorthand for `--set=*.attest=type=sbom`                                                                              |
| [`--set`](#set)                     | `stringArray` |         | Override target value (e.g., `targetpattern.key=value`)                                                               |
| `--var`                             | `stringArray` |         | Set a variable value (e.g., `name=value`)                                                                             |


<!---MARKER_GEN_END-->

## Description

Bake is a high-level build command. Each specified target runs in parallel
as part of the build.

Read [High-level build options with Bake](https://docs.docker.com/build/bake/)
guide for introduction to writing bake files.

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
(see [`docker buildx build --allow`](https://docs.docker.com/reference/cli/docker/buildx/build/#allow)),
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

Bake also supports `--allow=buildx.local.delete` to grant local outputs
permission to delete stale files when `mode=delete` is set.

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

By default, local directory build contexts in Bake files are resolved from the
current working directory. To opt in to resolving local directory build contexts
from the Bake file that defines each path, set
`BUILDX_BAKE_FILE_RELATIVE_PATHS=1`. Compose files use the first Compose file
directory as the base, which matches Compose project directory semantics. Use
the `cwd://` prefix for paths that should remain relative to the current working
directory.

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

The `--list=variables` option displays variables defined in the Bake file, including their descriptions and default values.

### Example: listing variables with descriptions

```hcl
variable "GO_VERSION" {
  default     = "1.22"
  description = "Go version used for building the application"
}
```

```console
$ docker buildx bake --list=variables

NAME          DESCRIPTION                                      DEFAULT
GO_VERSION    Go version used for building the application     1.22
```

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
$ docker buildx bake --set target.arg.mybuildarg=value
$ docker buildx bake --set target.platform=linux/arm64
$ docker buildx bake --set foo*.arg.mybuildarg=value    # overrides build arg for all targets starting with 'foo'
$ docker buildx bake --set *.platform=linux/arm64       # overrides platforms for all targets
$ docker buildx bake --set foo*.no-cache                # bypass caching only for targets starting with 'foo'
$ docker buildx bake --set target.platform+=linux/arm64 # appends 'linux/arm64' to the platform list
$ docker buildx bake --set target.contexts.bar=../bar   # overrides 'bar' named context
$ docker buildx bake --set target.resource.memory=2g    # overrides memory resource limit
$ docker buildx bake --set target.secret.aws=env=AWS    # overrides source for an existing secret
```

> [!NOTE]
>
> `--set` is a repeatable flag. For array fields such as `tag`, repeat `--set`
> to provide multiple values or use the `+=` operator to append without
> replacing. Array literal syntax like `--set target.tag=[a,b]` is not
> supported.

You can override the following fields:

* `annotation`
* `attest`
* `arg.<name>`
* `cache-from`
* `cache-to`
* `call`
* `context`
* `contexts.<name>`
* `dockerfile`
* `entitlement`
* `extra-host.<hostname>`
* `label.<name>`
* `load`
* `no-cache`
* `no-cache-filter`
* `network`
* `output`
* `platform`
* `policy`
* `pull`
* `push`
* `resource.<field>`
* `secret`
* `secret.<id>`
* `shm-size`
* `ssh`
* `tag`
* `target`
* `ulimit`

You can append using `+=` operator for the following fields:

* `annotation`¹
* `attest`¹
* `cache-from`
* `cache-to`
* `entitlement`¹
* `no-cache-filter`
* `output`
* `platform`
* `policy`
* `secret`
* `ssh`
* `tag`
* `ulimit`

> [!NOTE]
> ¹ These fields already append by default.

Override keys use singular names. Existing plural Bake field names and
historical `--set` spellings remain supported as aliases:

| Alias          | Canonical key |
|----------------|---------------|
| `annotations`  | `annotation`  |
| `args`         | `arg`         |
| `entitlements` | `entitlement` |
| `extra-hosts`  | `extra-host`  |
| `labels`       | `label`       |
| `platforms`    | `platform`    |
| `resources`    | `resource`    |
| `secrets`      | `secret`      |
| `tags`         | `tag`         |
| `ulimits`      | `ulimit`      |

Aliases are normalized before overrides are applied, so they can be mixed
with canonical keys in repeated `--set` options. `context` and `contexts` are
not aliases: `context` sets the build context, while `contexts.<name>` sets a
named context. `secret.<id>` also has separate semantics: it changes the
source of a secret that is already declared by the target. The plural alias
does not support this form; `secrets.<id>` is invalid.

#### Inline values for composable attributes

Composable fields such as `ssh`, `secret`, `output`, `cache-to`, `cache-from`,
`attest`, and `annotation` accept a list of object values in a Bake file. When
you override these fields with `--set`, provide each value using the same
inline string syntax as the corresponding build flag, not the HCL object
form. The override replaces or appends to the list as a whole and does not
address individual object fields with a subkey.

Only `arg`, `contexts`, `extra-host`, `label`, `resource`, and `secret` support
entry-addressed subkeys. For example, use `--set target.arg.MYARG=value` to
override one build argument. As described above, `secret.<id>` changes the
source of an existing secret rather than an object field.

For example, to set the SSH agent socket or key for a target, use the same
`id=path` form accepted by [`build --ssh`](buildx_build.md#ssh):

```console
$ docker buildx bake --set "*.ssh=default=$HOME/.ssh/id_ed25519"
```

To expose multiple paths for the same `id`, separate them with commas in the
second part:

```console
$ docker buildx bake --set "*.ssh=default=$HOME/.ssh/id_ed25519,$HOME/.ssh/id_rsa"
```

Your shell expands `$HOME` before buildx sees the value. The equivalent Bake
file definition uses the
[`homedir`](https://docs.docker.com/build/bake/stdlib/#homedir) HCL function:

```hcl
target "default" {
  ssh = [{ id = "default", paths = ["${homedir()}/.ssh/id_ed25519", "${homedir()}/.ssh/id_rsa"] }]
}
```
