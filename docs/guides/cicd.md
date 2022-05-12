# CI/CD

## GitHub Actions

Docker provides a [GitHub Action that will build and push your image](https://github.com/docker/build-push-action/#about)
using Buildx. Here is a simple workflow:

```yaml
name: ci

on:
  push:
    branches:
      - 'main'

jobs:
  docker:
    runs-on: ubuntu-latest
    steps:
      -
        name: Set up QEMU
        uses: docker/setup-qemu-action@v2
      -
        name: Set up Docker Buildx
        uses: docker/setup-buildx-action@v2
      -
        name: Login to DockerHub
        uses: docker/login-action@v2 
        with:
          username: ${{ secrets.DOCKERHUB_USERNAME }}
          password: ${{ secrets.DOCKERHUB_TOKEN }}
      -
        name: Build and push
        uses: docker/build-push-action@v2
        with:
          push: true
          tags: user/app:latest
```

In this example we are also using 3 other actions:

* [`setup-buildx`](https://github.com/docker/setup-buildx-action) action will create and boot a builder using by
default the `docker-container` [builder driver](../reference/buildx_create.md#driver).
This is **not required but recommended** using it to be able to build multi-platform images, export cache, etc.
* [`setup-qemu`](https://github.com/docker/setup-qemu-action) action can be useful if you want
to add emulation support with QEMU to be able to build against more platforms.
* [`login`](https://github.com/docker/login-action) action will take care to log
in against a Docker registry.
