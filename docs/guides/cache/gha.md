# GitHub Actions cache storage

> **Warning**
>
> The `gha` cache is currently experimental. You can use it today, in current
> releases of Buildx and Buildkit, however, the interface and behavior do not
> have any stability guarantees and may change in future releases.

The `gha` cache utilizes the [GitHub-provided Action's cache](https://github.com/actions/cache)
available from inside your CI execution environment. This is the recommended
cache to use inside your GitHub action pipelines, as long as your use case
falls within the [size and usage limits set by GitHub](https://docs.github.com/en/actions/using-workflows/caching-dependencies-to-speed-up-workflows#usage-limits-and-eviction-policy).

> **Note**
>
> The `gha` cache storage backend requires using a different driver than
> the default `docker` driver - see more information on selecting a driver
> [here](../drivers/index.md). To create a new docker-container driver (which
> can act as a simple drop-in replacement):
>
> ```console
> docker buildx create --use --driver=docker-container
> ```
>
> If you're using the official [docker/setup-buildx-action](https://github.com/docker/setup-buildx-action),
> then this step will be automatically run for you.

To import and export your cache using the `gha` storage backend we use the
`--cache-to` and `--cache-from` flags configured with the appropriate
[Authentication](#authentication) parameters:

```console
$ docker buildx build --push -t <user>/<image> \
  --cache-to type=gha,url=...,token=...
  --cache-from type=gha,url=...,token=...
```

By default, caches are scoped by branch - this ensures a separate cache
environment for the main branch, as well as for each feature branch. However,
if you build multiple images as part of your build, then caching them both to
the same `gha` scope will overwrite all but the last build, leaving only the
final cache.

To prevent this, you can manually specify a cache scope name using the `scope`
parameter (in this case, including the branch name set [by GitHub](https://docs.github.com/en/actions/learn-github-actions/environment-variables#default-environment-variables)
to ensure each branch gets its own cache):

```console
$ docker buildx build --push -t <user>/<image> \
  --cache-to type=gha,url=...,token=...,scope=$GITHUB_REF_NAME-image
  --cache-from type=gha,url=...,token=...,scope=$GITHUB_REF_NAME-image
$ docker buildx build --push -t <user>/<image2> \
  --cache-to type=gha,url=...,token=...,scope=$GITHUB_REF_NAME-image2
  --cache-from type=gha,url=...,token=...,scope=$GITHUB_REF_NAME-image2
```

GitHub's [cache scoping rules](https://docs.github.com/en/actions/advanced-guides/caching-dependencies-to-speed-up-workflows#restrictions-for-accessing-a-cache),
still apply, with the cache only populated from the current branch, the base
branch and the default branch for a run.

## Authentication

To authenticate against the [GitHub Actions Cache service API](https://github.com/tonistiigi/go-actions-cache/blob/master/api.md#authentication)
to read from and write to the cache, the following parameters are required:

* `url`: cache server URL (default `$ACTIONS_CACHE_URL`)
* `token`: access token (default `$ACTIONS_RUNTIME_TOKEN`)

If the parameters are not specified, then their values will be pulled from the
environment variables. If invoking the `docker buildx` command manually from an
inline step, then the variables must be manually exposed, for example, by using
[crazy-max/ghaction-github-runtime](https://github.com/crazy-max/ghaction-github-runtime)
as a workaround.

### With [docker/build-push-action](https://github.com/docker/build-push-action)

When using the [docker/build-push-action](https://github.com/docker/build-push-action),
the `url` and `token` parameters are automatically populated, with no need to
manually specify them, or include any additional workarounds.

For example:

```yaml
      -
        name: Build and push
        uses: docker/build-push-action@v3
        with:
          context: .
          push: true
          tags: user/app:latest
          cache-from: type=gha
          cache-to: type=gha,mode=max
```

<!-- FIXME: cross-link to ci docs once docs.docker.com has them -->

## Cache options

The `gha` cache has lots of parameters to adjust its behavior.

### Cache mode

See [Registry - Cache mode](./registry.md#cache-mode) for more information.

## Further reading

For an introduction to caching see [Optimizing builds with cache management](https://docs.docker.com/build/building/cache).

For more information on the `gha` cache backend, see the [BuildKit README](https://github.com/moby/buildkit#github-actions-cache-experimental).
