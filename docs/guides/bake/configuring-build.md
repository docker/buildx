---
title: "Configuring builds"
keywords: build, buildx, bake, buildkit, hcl, json
---

## Global scope attributes

You can define global scope attributes in HCL/JSON and use them for code reuse
and setting values for variables. This means you can do a "data-only" HCL file
with the values you want to set/override and use it in the list of regular
output files.

```hcl
# docker-bake.hcl
variable "FOO" {
  default = "abc"
}

target "app" {
  args = {
    v1 = "pre-${FOO}"
  }
}
```

You can use this file directly:

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
        "v1": "pre-abc"
      }
    }
  }
}
```

Or create an override configuration file:

```hcl
# env.hcl
WHOAMI="myuser"
FOO="def-${WHOAMI}"
```

And invoke bake together with both of the files:

```console
$ docker buildx bake -f docker-bake.hcl -f env.hcl --print app
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
        "v1": "pre-def-myuser"
      }
    }
  }
}
```

## From command line

You can also override target configurations from the command line with [`--set` flag](https://docs.docker.com/engine/reference/commandline/buildx_bake/#set):

```hcl
# docker-bake.hcl
target "app" {
  args = {
    mybuildarg = "foo"
  }
}
```

```console
$ docker buildx bake --set app.args.mybuildarg=bar --set app.platform=linux/arm64 app --print
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
        "mybuildarg": "bar"
      },
      "platforms": [
        "linux/arm64"
      ]
    }
  }
}
```

Pattern matching syntax defined in [https://golang.org/pkg/path/#Match](https://golang.org/pkg/path/#Match)
is also supported:

```console
$ docker buildx bake --set foo*.args.mybuildarg=value  # overrides build arg for all targets starting with "foo"
$ docker buildx bake --set *.platform=linux/arm64      # overrides platform for all targets
$ docker buildx bake --set foo*.no-cache               # bypass caching only for targets starting with "foo"
```

Complete list of overridable fields:

* `args`
* `cache-from`
* `cache-to`
* `context`
* `dockerfile`
* `labels`
* `no-cache`
* `output`
* `platform`
* `pull`
* `secrets`
* `ssh`
* `tags`
* `target`
