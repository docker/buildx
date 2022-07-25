# Docker container driver

The buildx docker-container driver allows creation of a managed and
customizable BuildKit environment inside a dedicated Docker container.

Using the docker-container driver has a couple of advantages over the basic
docker driver. Firstly, we can manually override the version of buildkit to
use, meaning that we can access the latest and greatest features as soon as
they're released, instead of waiting to upgrade to a newer version of Docker.
Additionally, we can access more complex features like multi-architecture
builds and the more advanced cache exporters, which are currently unsupported
in the default docker driver.

We can easily create a new builder that uses the docker-container driver:

```console
$ docker buildx create --name container --driver docker-container
container
```

We should then be able to see it on our list of available builders:

```console
$ docker buildx ls
NAME/NODE       DRIVER/ENDPOINT      STATUS   BUILDKIT PLATFORMS
container       docker-container                       
  container0    desktop-linux        inactive          
default         docker                                 
  default       default              running  20.10.17 linux/amd64, linux/386
```

If we trigger a build, the appropriate `moby/buildkit` image will be pulled
from [Docker Hub](https://hub.docker.com/u/moby/buildkit), the image started,
and our build submitted to our containerized build server.

```console
$ docker buildx build -t <image> --builder=container .
WARNING: No output specified with docker-container driver. Build result will only remain in the build cache. To push result image into registry use --push or to load image into docker use --load
#1 [internal] booting buildkit
#1 pulling image moby/buildkit:buildx-stable-1
#1 pulling image moby/buildkit:buildx-stable-1 1.9s done
#1 creating container buildx_buildkit_container0
#1 creating container buildx_buildkit_container0 0.5s done
#1 DONE 2.4s
...
```

Note the warning "Build result will only remain in the build cache" - unlike
the `docker` driver, the built image must be explicitly loaded into the local
image store. We can use the `--load` flag for this:

```console
$ docker buildx build --load -t <image> --builder=container .
...
 => exporting to oci image format                                                                                                      7.7s
 => => exporting layers                                                                                                                4.9s
 => => exporting manifest sha256:4e4ca161fa338be2c303445411900ebbc5fc086153a0b846ac12996960b479d3                                      0.0s 
 => => exporting config sha256:adf3eec768a14b6e183a1010cb96d91155a82fd722a1091440c88f3747f1f53f                                        0.0s 
 => => sending tarball                                                                                                                 2.8s 
 => importing to docker   
```

The image should then be available in the image store:

```console
$ docker image ls
REPOSITORY                       TAG               IMAGE ID       CREATED             SIZE
<image>                          latest            adf3eec768a1   2 minutes ago       197MB
```
