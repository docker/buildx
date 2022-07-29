# Buildx drivers overview

The buildx client connects out to the BuildKit backend to execute builds -
Buildx drivers allow fine-grained control over management of the backend, and
supports several different options for where and how BuildKit should run.

Currently, we support the following drivers:

- The `docker` driver, that uses the BuildKit library bundled into the Docker
  daemon.
  ([guide](./docker.md), [reference](https://docs.docker.com/engine/reference/commandline/buildx_create/#driver))
- The `docker-container` driver, that launches a dedicated BuildKit container
  using Docker, for access to advanced features.
  ([guide](./docker-container.md), [reference](https://docs.docker.com/engine/reference/commandline/buildx_create/#driver))
- The `kubernetes` driver, that launches dedicated BuildKit pods in a
  remote Kubernetes cluster, for scalable builds.
  ([guide](./kubernetes.md), [reference](https://docs.docker.com/engine/reference/commandline/buildx_create/#driver))
- The `remote` driver, that allows directly connecting to a manually managed
  BuildKit daemon, for more custom setups.
  ([guide](./remote.md))

<!--- FIXME: for 0.9, make links relative, and add reference link for remote --->

To create a new builder that uses one of the above drivers, you can use the
[`docker buildx create`](https://docs.docker.com/engine/reference/commandline/buildx_create/) command:

```console
$ docker buildx create --name=<builder-name> --driver=<driver> --driver-opt=<driver-options>
```

The build experience is very similar across drivers, however, there are some
features that are not evenly supported across the board, notably, the `docker`
driver does not include support for certain output/caching types.

| Feature                       |    `docker`     | `docker-container` | `kubernetes` |        `remote`        |
| :---------------------------- | :-------------: | :----------------: | :----------: | :--------------------: |
| **Automatic `--load`**        |        ✅        |         ❌          |      ❌       |           ❌            |
| **Cache export**              | ❔ (inline only) |         ✅          |      ✅       |           ✅            |
| **Docker/OCI tarball output** |        ❌        |         ✅          |      ✅       |           ✅            |
| **Multi-arch images**         |        ❌        |         ✅          |      ✅       |           ✅            |
| **BuildKit configuration**    |        ❌        |         ✅          |      ✅       | ❔ (managed externally) |
