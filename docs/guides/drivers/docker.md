# Docker driver

The buildx docker driver is the default builtin driver, that uses the BuildKit
server components built directly into the docker engine.

No setup should be required for the docker driver - it should already be
configured for you:

```console
$ docker buildx ls
NAME/NODE       DRIVER/ENDPOINT      STATUS   BUILDKIT PLATFORMS
default         docker                                 
  default       default              running  20.10.17 linux/amd64, linux/386
```

This builder is ready to build with out-of-the-box, requiring no extra setup,
so you can get going with a `docker buildx build` as soon as you like.

Depending on your personal setup, you may find multiple builders in your list
the use the docker driver. For example, on a system that runs both a package
managed version of dockerd, as well as Docker Desktop, you might have the
following:

```console
NAME/NODE       DRIVER/ENDPOINT STATUS  BUILDKIT PLATFORMS
default         docker                           
  default       default         running 20.10.17 linux/amd64, linux/386
desktop-linux * docker                           
  desktop-linux desktop-linux   running 20.10.17 linux/amd64, linux/arm64, linux/riscv64, linux/ppc64le, linux/s390x, linux/386, linux/arm/v7, linux/arm/v6
```

This is because the docker driver builders are automatically pulled from
the available [Docker Contexts](https://docs.docker.com/engine/context/working-with-contexts/).
When you add new contexts using `docker context create`, these will appear in
your list of buildx builders.

Unlike the [other drivers](./index.md), builders using the docker driver
cannot be manually created, and can only be automatically created from the
docker context. Additionally, they cannot be configured to a specific BuildKit
version, and cannot take any extra parameters, as these are both preset by the
Docker engine internally.

If you want the extra configuration and flexibility without too much more
overhead, then see the help page for the [docker-container driver](./docker-container.md).

## Further reading

For more information on the docker driver, see the [buildx reference](https://docs.docker.com/engine/reference/commandline/buildx_create/#driver).
