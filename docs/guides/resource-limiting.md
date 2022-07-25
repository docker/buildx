# Resource limiting

## Max parallelism

You can limit the parallelism of the BuildKit solver, which is particularly useful
for low-powered machines, using a [BuildKit daemon configuration](https://github.com/moby/buildkit/blob/master/docs/buildkitd.toml.md)
while creating a builder with the [`--config` flags](https://docs.docker.com/engine/reference/commandline/buildx_create/#config).

```toml
# /etc/buildkitd.toml
[worker.oci]
  max-parallelism = 4
```

Now you can [create a `docker-container` builder](https://docs.docker.com/engine/reference/commandline/buildx_create/)
that will use this BuildKit configuration to limit parallelism.

```console
$ docker buildx create --use \
  --name mybuilder \
  --driver docker-container \
  --config /etc/buildkitd.toml
```

## Limit on TCP connections

We are also now limiting TCP connections to **4 per registry** with an additional
connection not used for layer pulls and pushes. This limitation will be able to
manage TCP connection per host to avoid your build being stuck while pulling
images. The additional connection is used for metadata requests
(image config retrieval) to enhance the overall build time.

More info: [moby/buildkit#2259](https://github.com/moby/buildkit/pull/2259)
