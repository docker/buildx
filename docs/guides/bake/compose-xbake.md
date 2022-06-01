---
title: "Extension field with Compose"
keywords: build, buildx, bake, buildkit, compose
---

[Special extension](https://docs.docker.com/compose/compose-file/#extension)
field `x-bake` can be used in your compose file to evaluate fields that are not
(yet) available in the [build definition](https://docs.docker.com/compose/compose-file/build/#build-definition).

```yaml
# docker-compose.yml
services:
  addon:
    image: ct-addon:bar
    build:
      context: .
      dockerfile: ./Dockerfile
      args:
        CT_ECR: foo
        CT_TAG: bar
      x-bake:
        tags:
          - ct-addon:foo
          - ct-addon:alp
        platforms:
          - linux/amd64
          - linux/arm64
        cache-from:
          - user/app:cache
          - type=local,src=path/to/cache
        cache-to: type=local,dest=path/to/cache
        pull: true

  aws:
    image: ct-fake-aws:bar
    build:
      dockerfile: ./aws.Dockerfile
      args:
        CT_ECR: foo
        CT_TAG: bar
      x-bake:
        secret:
          - id=mysecret,src=./secret
          - id=mysecret2,src=./secret2
        platforms: linux/arm64
        output: type=docker
        no-cache: true
```

```console
$ docker buildx bake --print
```
```json
{
  "group": {
    "default": {
      "targets": [
        "aws",
        "addon"
      ]
    }
  },
  "target": {
    "addon": {
      "context": ".",
      "dockerfile": "./Dockerfile",
      "args": {
        "CT_ECR": "foo",
        "CT_TAG": "bar"
      },
      "tags": [
        "ct-addon:foo",
        "ct-addon:alp"
      ],
      "cache-from": [
        "user/app:cache",
        "type=local,src=path/to/cache"
      ],
      "cache-to": [
        "type=local,dest=path/to/cache"
      ],
      "platforms": [
        "linux/amd64",
        "linux/arm64"
      ],
      "pull": true
    },
    "aws": {
      "context": ".",
      "dockerfile": "./aws.Dockerfile",
      "args": {
        "CT_ECR": "foo",
        "CT_TAG": "bar"
      },
      "tags": [
        "ct-fake-aws:bar"
      ],
      "secret": [
        "id=mysecret,src=./secret",
        "id=mysecret2,src=./secret2"
      ],
      "platforms": [
        "linux/arm64"
      ],
      "output": [
        "type=docker"
      ],
      "no-cache": true
    }
  }
}
```

Complete list of valid fields for `x-bake`:

* `cache-from`
* `cache-to`
* `no-cache`
* `no-cache-filter`
* `output`
* `platforms`
* `pull`
* `secret`
* `ssh`
* `tags`
