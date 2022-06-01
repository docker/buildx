---
title: "Bake file definition"
keywords: build, buildx, bake, buildkit, hcl, json, compose
---

`buildx bake` supports HCL, JSON and Compose file format for defining build
groups and targets. It looks for build definition files in the current
directory in the following order:

* `docker-compose.yml`
* `docker-compose.yaml`
* `docker-bake.json`
* `docker-bake.override.json`
* `docker-bake.hcl`
* `docker-bake.override.hcl`

A target reflects a single docker build invocation with the same options that
you would specify for `docker build`. A group is a grouping of targets.

Multiple files can include the same target and final build options will be
determined by merging them together.

> **Note**
>
> In the case of compose files, each service corresponds to a target.

A group can specify its list of targets with the `targets` option. A target can
inherit build options by setting the `inherits` option to the list of targets or
groups to inherit from.

## HCL definition

HCL definition file is recommended as its experience is more aligned with buildx UX
and also allows better code reuse, different target groups and extended features.

```hcl
# docker-bake.hcl
variable "TAG" {
  default = "latest"
}

group "default" {
  targets = ["db", "webapp-dev"]
}

target "webapp-dev" {
  dockerfile = "Dockerfile.webapp"
  tags = ["docker.io/username/webapp:${TAG}"]
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

* `args`
* `cache-from`
* `cache-to`
* `context`
* `contexts`
* `dockerfile`
* `inherits`
* `labels`
* `no-cache`
* `no-cache-filter`
* `output`
* `platform`
* `pull`
* `secrets`
* `ssh`
* `tags`
* `target`

## JSON definition

```json
{
  "variable": {
    "TAG": {
      "default": "latest"
    }
  },
  "group": {
    "default": {
      "targets": [
        "db",
        "webapp-dev"
      ]
    }
  },
  "target": {
    "webapp-dev": {
      "dockerfile": "Dockerfile.webapp",
      "tags": [
        "docker.io/username/webapp:${TAG}"
      ]
    },
    "webapp-release": {
      "inherits": [
        "webapp-dev"
      ],
      "platforms": [
        "linux/amd64",
        "linux/arm64"
      ]
    },
    "db": {
      "dockerfile": "Dockerfile.db",
      "tags": [
        "docker.io/username/db"
      ]
    }
  }
}
```

Same list of target fields as [HCL definition](#hcl-definition) are available.

## Compose file

```yaml
# docker-compose.yml
services:
  webapp-dev: &dev
    build:
      dockerfile: Dockerfile.webapp
      tags:
        - docker.io/username/webapp:latest

  webapp-release:
    <<: *dev
    build:
      x-bake:
        platforms:
          - linux/amd64
          - linux/arm64

  db:
    image: docker.io/username/db
    build:
      dockerfile: Dockerfile.db
```

> **Limitations**
>
> Bake uses the [compose-spec](https://docs.docker.com/compose/compose-file/) to
> parse a compose file. Some fields are not (yet) available, but you can use
> the [special extension field `x-bake`](compose-xbake.md).
>
> `inherits` service field is also not supported. Use [YAML anchors](https://docs.docker.com/compose/compose-file/#fragments)
> to reference other services.
>
> Specifying variables or global scope attributes is not yet supported for
> compose files.
{: .warning }

## Remote definition

You can also use a remote `git` bake definition:

```console
$ docker buildx bake "https://github.com/docker/cli.git#v20.10.11" --print
#1 [internal] load git source https://github.com/docker/cli.git#v20.10.11
#1 0.745 e8f1871b077b64bcb4a13334b7146492773769f7       refs/tags/v20.10.11
#1 2.022 From https://github.com/docker/cli
#1 2.022  * [new tag]         v20.10.11  -> v20.10.11
#1 DONE 2.9s
{
  "group": {
    "default": {
      "targets": [
        "binary"
      ]
    }
  },
  "target": {
    "binary": {
      "context": "https://github.com/docker/cli.git#v20.10.11",
      "dockerfile": "Dockerfile",
      "args": {
        "BASE_VARIANT": "alpine",
        "GO_STRIP": "",
        "VERSION": ""
      },
      "target": "binary",
      "platforms": [
        "local"
      ],
      "output": [
        "build"
      ]
    }
  }
}
```

As you can see the context is fixed to `https://github.com/docker/cli.git` even if
[no context is actually defined](https://github.com/docker/cli/blob/2776a6d694f988c0c1df61cad4bfac0f54e481c8/docker-bake.hcl#L17-L26)
in the definition.

If you want to access the main context for bake command from a bake file
that has been imported remotely, you can use the [`BAKE_CMD_CONTEXT` built-in var](hcl-vars-funcs.md#built-in-variables).

```console
$ cat https://raw.githubusercontent.com/tonistiigi/buildx/remote-test/docker-bake.hcl
```
```hcl
target "default" {
  context = BAKE_CMD_CONTEXT
  dockerfile-inline = <<EOT
FROM alpine
WORKDIR /src
COPY . .
RUN ls -l && stop
EOT
}
```

```console
$ docker buildx bake "https://github.com/tonistiigi/buildx.git#remote-test" --print
```
```json
{
  "target": {
    "default": {
      "context": ".",
      "dockerfile": "Dockerfile",
      "dockerfile-inline": "FROM alpine\nWORKDIR /src\nCOPY . .\nRUN ls -l \u0026\u0026 stop\n"
    }
  }
}
```

```console
$ touch foo bar
$ docker buildx bake "https://github.com/tonistiigi/buildx.git#remote-test"
...
 > [4/4] RUN ls -l && stop:
#8 0.101 total 0
#8 0.102 -rw-r--r--    1 root     root             0 Jul 27 18:47 bar
#8 0.102 -rw-r--r--    1 root     root             0 Jul 27 18:47 foo
#8 0.102 /bin/sh: stop: not found
```

```console
$ docker buildx bake "https://github.com/tonistiigi/buildx.git#remote-test" "https://github.com/docker/cli.git#v20.10.11" --print
#1 [internal] load git source https://github.com/tonistiigi/buildx.git#remote-test
#1 0.429 577303add004dd7efeb13434d69ea030d35f7888       refs/heads/remote-test
#1 CACHED
{
  "target": {
    "default": {
      "context": "https://github.com/docker/cli.git#v20.10.11",
      "dockerfile": "Dockerfile",
      "dockerfile-inline": "FROM alpine\nWORKDIR /src\nCOPY . .\nRUN ls -l \u0026\u0026 stop\n"
    }
  }
}
```

```console
$ docker buildx bake "https://github.com/tonistiigi/buildx.git#remote-test" "https://github.com/docker/cli.git#v20.10.11"
...
 > [4/4] RUN ls -l && stop:
#8 0.136 drwxrwxrwx    5 root     root          4096 Jul 27 18:31 kubernetes
#8 0.136 drwxrwxrwx    3 root     root          4096 Jul 27 18:31 man
#8 0.136 drwxrwxrwx    2 root     root          4096 Jul 27 18:31 opts
#8 0.136 -rw-rw-rw-    1 root     root          1893 Jul 27 18:31 poule.yml
#8 0.136 drwxrwxrwx    7 root     root          4096 Jul 27 18:31 scripts
#8 0.136 drwxrwxrwx    3 root     root          4096 Jul 27 18:31 service
#8 0.136 drwxrwxrwx    2 root     root          4096 Jul 27 18:31 templates
#8 0.136 drwxrwxrwx   10 root     root          4096 Jul 27 18:31 vendor
#8 0.136 -rwxrwxrwx    1 root     root          9620 Jul 27 18:31 vendor.conf
#8 0.136 /bin/sh: stop: not found
```

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
