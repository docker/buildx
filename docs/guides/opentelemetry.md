# OpenTelemetry support

To capture the trace to [Jaeger](https://github.com/jaegertracing/jaeger), set
`JAEGER_TRACE` environment variable to the collection address using a `driver-opt`.

First create a Jaeger container:

```console
$ docker run -d --name jaeger -p "6831:6831/udp" -p "16686:16686" jaegertracing/all-in-one
```

Then [create a `docker-container` builder](../reference/buildx_create.md)
that will use the Jaeger instance via the `JAEGER_TRACE` env var:

```console
$ docker buildx create --use \
  --name mybuilder \
  --driver docker-container \
  --driver-opt "network=host" \
  --driver-opt "env.JAEGER_TRACE=localhost:6831"
```

Boot and [inspect `mybuilder`](../reference/buildx_inspect.md):

```console
$ docker buildx inspect --bootstrap
```

Buildx commands should be traced at `http://127.0.0.1:16686/`:

![](https://user-images.githubusercontent.com/1951866/124468052-ef085400-dd98-11eb-84ab-7ac8e261dd52.png)
