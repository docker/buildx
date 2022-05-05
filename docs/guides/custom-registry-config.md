---
title: "Using a custom registry configuration"
description: "Set registry configuration in your builder"
keywords: build, buildx, buildkit, registry
---

If you [create a `docker-container` or `kubernetes` builder](https://docs.docker.com/engine/reference/commandline/buildx_create/) and
have specified certificates for registries in the [BuildKit daemon configuration](https://github.com/moby/buildkit/blob/master/docs/buildkitd.toml.md),
the files will be copied into the container under `/etc/buildkit/certs` and
configuration will be updated to reflect that.

Take the following `buildkitd.toml` configuration that will be used for
pushing an image to this registry using self-signed certificates:

```toml
# /etc/buildkitd.toml
debug = true
[registry."myregistry.com"]
  ca=["/etc/certs/myregistry.pem"]
  [[registry."myregistry.com".keypair]]
    key="/etc/certs/myregistry_key.pem"
    cert="/etc/certs/myregistry_cert.pem"
```

Here we have configured a self-signed certificate for `myregistry.com` registry.

Now [create a `docker-container` builder](https://docs.docker.com/engine/reference/commandline/buildx_create/)
that will use this BuildKit configuration:

```console
$ docker buildx create --use \
  --name mybuilder \
  --driver docker-container \
  --config /etc/buildkitd.toml
```

Inspecting the builder container, you can see that buildkitd configuration
has changed:

```console
$ docker exec -it buildx_buildkit_mybuilder0 cat /etc/buildkit/buildkitd.toml
```
```toml
debug = true

[registry]

  [registry."myregistry.com"]
    ca = ["/etc/buildkit/certs/myregistry.com/myregistry.pem"]

    [[registry."myregistry.com".keypair]]
      cert = "/etc/buildkit/certs/myregistry.com/myregistry_cert.pem"
      key = "/etc/buildkit/certs/myregistry.com/myregistry_key.pem"
```

And certificates copied inside the container:

```console
$ docker exec -it buildx_buildkit_mybuilder0 ls /etc/buildkit/certs/myregistry.com/
myregistry.pem    myregistry_cert.pem   myregistry_key.pem
```

Now you should be able to push to the registry with this builder:

```console
$ docker buildx build --push --tag myregistry.com/myimage:latest .
```
