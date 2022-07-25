# Using a custom network

[Create a network](https://docs.docker.com/engine/reference/commandline/network_create/)
named `foonet`:

```console
$ docker network create foonet
```

[Create a `docker-container` builder](https://docs.docker.com/engine/reference/commandline/buildx_create/)
named `mybuilder` that will use this network:

```console
$ docker buildx create --use \
  --name mybuilder \
  --driver docker-container \
  --driver-opt "network=foonet"
```

Boot and [inspect `mybuilder`](https://docs.docker.com/engine/reference/commandline/buildx_inspect/):

```console
$ docker buildx inspect --bootstrap
```

[Inspect the builder container](https://docs.docker.com/engine/reference/commandline/inspect/)
and see what network is being used:

{% raw %}
```console
$ docker inspect buildx_buildkit_mybuilder0 --format={{.NetworkSettings.Networks}}
map[foonet:0xc00018c0c0]
```
{% endraw %}
