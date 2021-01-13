# `buildx bake [OPTIONS] [TARGET...]`

Bake is a high-level build command.

Each specified target will run in parallel as part of the build.

Options:

| Flag | Description |
| --- | --- |
|  -f, --file stringArray  | Build definition file
|      --load              | Shorthand for --set=*.output=type=docker
|      --no-cache          | Do not use cache when building the image
|      --print             | Print the options without building
|      --progress string   | Set type of progress output (auto, plain, tty). Use plain to show container output (default "auto")
|      --pull              | Always attempt to pull a newer version of the image
|      --push              | Shorthand for --set=*.output=type=registry
|      --set stringArray   | Override target value (eg: targetpattern.key=value)


## Description

Bake is a high-level build command. Each specified target will run in parallel
as part of the build.


### `-f, --file FILE`

Specifies the bake definition file. The file can be a Docker Compose, JSON or HCL
file. If multiple files are specified they are all read and configurations are
combined. By default, if no files are specified, the following are parsed:

- `docker-compose.yml`
- `docker-compose.yaml`
- `docker-bake.json`
- `docker-bake.override.json`
- `docker-bake.hcl`
- `docker-bake.override.hcl`

### `--no-cache`

Same as `build --no-cache`. Do not use cache when building the image.

### `--print`

Prints the resulting options of the targets desired to be built, in a JSON format,
without starting a build.

```console
$ docker buildx bake -f docker-bake.hcl --print db
{
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

### `--progress`

Same as `build --progress`. Set type of progress output (auto, plain, tty). Use
plain to show container output (default "auto").

### `--pull`

Same as `build --pull`.

### `--set targetpattern.key[.subkey]=value`

Override target configurations from command line. The pattern matching syntax is
defined in https://golang.org/pkg/path/#Match.

Example:

```console
docker buildx bake --set target.args.mybuildarg=value
docker buildx bake --set target.platform=linux/arm64
docker buildx bake --set foo*.args.mybuildarg=value # overrides build arg for all targets starting with 'foo'
docker buildx bake --set *.platform=linux/arm64     # overrides platform for all targets
docker buildx bake --set foo*.no-cache              # bypass caching only for targets starting with 'foo'
```

Complete list of overridable fields:
args, cache-from, cache-to, context, dockerfile, labels, no-cache, output, platform,
pull, secrets, ssh, tags, target

### File definition

In addition to compose files, bake supports a JSON and an equivalent HCL file
format for defining build groups and targets.

A target reflects a single docker build invocation with the same options that
you would specify for `docker build`. A group is a grouping of targets.

Multiple files can include the same target and final build options will be
determined by merging them together.

In the case of compose files, each service corresponds to a target.

A group can specify its list of targets with the `targets` option. A target can
inherit build options by setting the `inherits` option to the list of targets or
groups to inherit from.

Note: Design of bake command is work in progress, the user experience may change
based on feedback.


Example HCL definition:

```hcl
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

Complete list of valid target fields:
args, cache-from, cache-to, context, dockerfile, inherits, labels, no-cache,
output, platform, pull, secrets, ssh, tags, target

### HCL variables and functions

Similar to how Terraform provides a way to [define variables](https://www.terraform.io/docs/configuration/variables.html#declaring-an-input-variable),
the HCL file format also supports variable block definitions. These can be used
to define variables with values provided by the current environment, or a default
value when unset.


Example of using interpolation to tag an image with the git sha:

```console
$ cat <<'EOF' > docker-bake.hcl
variable "TAG" {
    default = "latest"
}

group "default" {
    targets = ["webapp"]
}

target "webapp" {
    tags = ["docker.io/username/webapp:${TAG}"]
}
EOF

$ docker buildx bake --print webapp
{
   "target": {
      "webapp": {
         "context": ".",
         "dockerfile": "Dockerfile",
         "tags": [
            "docker.io/username/webapp:latest"
         ]
      }
   }
}

$ TAG=$(git rev-parse --short HEAD) docker buildx bake --print webapp
{
   "target": {
      "webapp": {
         "context": ".",
         "dockerfile": "Dockerfile",
         "tags": [
            "docker.io/username/webapp:985e9e9"
         ]
      }
   }
}
```


A [set of generally useful functions](https://github.com/docker/buildx/blob/master/bake/hcl.go#L19-L65)
provided by [go-cty](https://github.com/zclconf/go-cty/tree/master/cty/function/stdlib)
are available for use in HCL files. In addition, [user defined functions](https://github.com/hashicorp/hcl/tree/hcl2/ext/userfunc)
are also supported.

Example of using the `add` function:

```console
$ cat <<'EOF' > docker-bake.hcl
variable "TAG" {
    default = "latest"
}

group "default" {
    targets = ["webapp"]
}

target "webapp" {
    args = {
        buildno = "${add(123, 1)}"
    }
}
EOF

$ docker buildx bake --print webapp
{
   "target": {
      "webapp": {
         "context": ".",
         "dockerfile": "Dockerfile",
         "args": {
            "buildno": "124"
         }
      }
   }
}
```

Example of defining an `increment` function:

```console
$ cat <<'EOF' > docker-bake.hcl
function "increment" {
    params = [number]
    result = number + 1
}

group "default" {
    targets = ["webapp"]
}

target "webapp" {
    args = {
        buildno = "${increment(123)}"
    }
}
EOF

$ docker buildx bake --print webapp
{
   "target": {
      "webapp": {
         "context": ".",
         "dockerfile": "Dockerfile",
         "args": {
            "buildno": "124"
         }
      }
   }
}
```

Example of only adding tags if a variable is not empty using an `notequal`
function:

```console
$ cat <<'EOF' > docker-bake.hcl
variable "TAG" {default="" }

group "default" {
    targets = [
        "webapp",
    ]
}

target "webapp" {
    context="."
    dockerfile="Dockerfile"
    tags = [
        "my-image:latest",
        notequal("",TAG) ? "my-image:${TAG}": "",
    ]
}
EOF

$ docker buildx bake --print webapp
{
   "target": {
      "webapp": {
         "context": ".",
         "dockerfile": "Dockerfile",
         "tags": [
            "my-image:latest"
         ]
      }
   }
}
```
