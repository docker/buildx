---
title: "HCL variables and functions"
keywords: build, buildx, bake, buildkit, hcl
---

Similar to how Terraform provides a way to [define variables](https://www.terraform.io/docs/configuration/variables.html#declaring-an-input-variable),
the HCL file format also supports variable block definitions. These can be used
to define variables with values provided by the current environment, or a
default value when unset.

A [set of generally useful functions](https://github.com/docker/buildx/blob/master/bake/hclparser/stdlib.go)
provided by [go-cty](https://github.com/zclconf/go-cty/tree/main/cty/function/stdlib)
are available for use in HCL files. In addition, [user defined functions](https://github.com/hashicorp/hcl/tree/main/ext/userfunc)
are also supported.

## Using interpolation to tag an image with the git sha

Bake supports variable blocks which are assigned to matching environment
variables or default values.

```hcl
# docker-bake.hcl
variable "TAG" {
  default = "latest"
}

group "default" {
  targets = ["webapp"]
}

target "webapp" {
  tags = ["docker.io/username/webapp:${TAG}"]
}
```

alternatively, in json format:

```json
{
  "variable": {
    "TAG": {
      "default": "latest"
    }
  },
  "group": {
    "default": {
      "targets": ["webapp"]
    }
  },
  "target": {
    "webapp": {
      "tags": ["docker.io/username/webapp:${TAG}"]
    }
  }
}
```

```console
$ docker buildx bake --print webapp
```
```json
{
  "group": {
    "default": {
      "targets": [
        "webapp"
      ]
    }
  },
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
```

```console
$ TAG=$(git rev-parse --short HEAD) docker buildx bake --print webapp
```
```json
{
  "group": {
    "default": {
      "targets": [
        "webapp"
      ]
    }
  },
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

## Using the `add` function

You can use [`go-cty` stdlib functions](https://github.com/zclconf/go-cty/tree/main/cty/function/stdlib).
Here we are using the `add` function.

```hcl
# docker-bake.hcl
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
```

```console
$ docker buildx bake --print webapp
```
```json
{
  "group": {
    "default": {
      "targets": [
        "webapp"
      ]
    }
  },
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

## Defining an `increment` function

It also supports [user defined functions](https://github.com/hashicorp/hcl/tree/main/ext/userfunc).
The following example defines a simple an `increment` function.

```hcl
# docker-bake.hcl
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
```

```console
$ docker buildx bake --print webapp
```
```json
{
  "group": {
    "default": {
      "targets": [
        "webapp"
      ]
    }
  },
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

## Only adding tags if a variable is not empty using an `notequal`

Here we are using the conditional `notequal` function which is just for
symmetry with the `equal` one.

```hcl
# docker-bake.hcl
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
```

```console
$ docker buildx bake --print webapp
```
```json
{
  "group": {
    "default": {
      "targets": [
        "webapp"
      ]
    }
  },
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

## Using variables in functions

You can refer variables to other variables like the target blocks can. Stdlib
functions can also be called but user functions can't at the moment.

```hcl
# docker-bake.hcl
variable "REPO" {
  default = "user/repo"
}

function "tag" {
  params = [tag]
  result = ["${REPO}:${tag}"]
}

target "webapp" {
  tags = tag("v1")
}
```

```console
$ docker buildx bake --print webapp
```
```json
{
  "group": {
    "default": {
      "targets": [
        "webapp"
      ]
    }
  },
  "target": {
    "webapp": {
      "context": ".",
      "dockerfile": "Dockerfile",
      "tags": [
        "user/repo:v1"
      ]
    }
  }
}
```

## Using variables in variables across files

When multiple files are specified, one file can use variables defined in
another file.

```hcl
# docker-bake1.hcl
variable "FOO" {
  default = upper("${BASE}def")
}

variable "BAR" {
  default = "-${FOO}-"
}

target "app" {
  args = {
    v1 = "pre-${BAR}"
  }
}
```

```hcl
# docker-bake2.hcl
variable "BASE" {
  default = "abc"
}

target "app" {
  args = {
    v2 = "${FOO}-post"
  }
}
```

```console
$ docker buildx bake -f docker-bake1.hcl -f docker-bake2.hcl --print app
```
```json
{
  "group": {
    "default": {
      "targets": [
        "app"
      ]
    }
  },
  "target": {
    "app": {
      "context": ".",
      "dockerfile": "Dockerfile",
      "args": {
        "v1": "pre--ABCDEF-",
        "v2": "ABCDEF-post"
      }
    }
  }
}
```

## Using typed variables

Non-string variables are also accepted. The value passed with env is parsed
into suitable type first.

```hcl
# docker-bake.hcl
variable "FOO" {
  default = 3
}

variable "IS_FOO" {
  default = true
}

target "app" {
  args = {
    v1 = FOO > 5 ? "higher" : "lower" 
    v2 = IS_FOO ? "yes" : "no"
  }
}
```

```console
$ docker buildx bake --print app
```
```json
{
  "group": {
    "default": {
      "targets": [
        "app"
      ]
    }
  },
  "target": {
    "app": {
      "context": ".",
      "dockerfile": "Dockerfile",
      "args": {
        "v1": "lower",
        "v2": "yes"
      }
    }
  }
}
```

## Built-in variables

* `BAKE_CMD_CONTEXT` can be used to access the main `context` for bake command
  from a bake file that has been [imported remotely](file-definition.md#remote-definition).
* `BAKE_LOCAL_PLATFORM` returns the current platform's default platform
  specification (e.g. `linux/amd64`).
