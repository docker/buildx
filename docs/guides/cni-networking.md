# CNI networking

It can be useful to use a bridge network for your builder if for example you
encounter a network port contention during multiple builds. If you're using
the BuildKit image, CNI is not yet available in it, but you can create
[a custom BuildKit image with CNI support](https://github.com/moby/buildkit/blob/master/docs/cni-networking.md).

Now build this image:

```console
$ docker buildx build --tag buildkit-cni:local --load .
```

Then [create a `docker-container` builder](https://docs.docker.com/engine/reference/commandline/buildx_create/) that
will use this image:

```console
$ docker buildx create --use \
  --name mybuilder \
  --driver docker-container \
  --driver-opt "image=buildkit-cni:local" \
  --buildkitd-flags "--oci-worker-net=cni"
```
