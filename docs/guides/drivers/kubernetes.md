# Kubernetes driver

The buildx kubernetes driver allows connecting your local development or ci
environments to your kubernetes cluster to allow access to more powerful
and varied compute resources.

This guide assumes you already have an existing kubernetes cluster - if you don't already
have one, you can easily follow along by installing
[minikube](https://minikube.sigs.k8s.io/docs/).

Before connecting buildx to your cluster, you may want to create a dedicated
namespace using `kubectl` to keep your buildx-managed resources separate. You
can call your namespace anything you want, or use the existing `default`
namespace, but we'll create a `buildkit` namespace for now:

```console
$ kubectl create namespace buildkit
```

Then create a new buildx builder:

```console
$ docker buildx create \
  --bootstrap \
  --name=kube \
  --driver=kubernetes \
  --driver-opt=namespace=buildkit
```

This assumes that the kubernetes cluster you want to connect to is currently
accessible via the kubectl command, with the `KUBECONFIG` environment variable
[set appropriately](https://kubernetes.io/docs/tasks/access-application-cluster/configure-access-multiple-clusters/#set-the-kubeconfig-environment-variable)
if neccessary.

You should now be able to see the builder in the list of buildx builders:

```console
$ docker buildx ls
NAME/NODE                DRIVER/ENDPOINT STATUS  PLATFORMS
kube                     kubernetes              
  kube0-6977cdcb75-k9h9m                 running linux/amd64, linux/amd64/v2, linux/amd64/v3, linux/386
default *                docker
  default                default         running linux/amd64, linux/386
```

The buildx driver creates the neccessary resources on your cluster in the
specified namespace (in this case, `buildkit`), while keeping your
driver configuration locally. You can see the running pods with:

```console
$ kubectl -n buildkit get deployments
NAME    READY   UP-TO-DATE   AVAILABLE   AGE
kube0   1/1     1            1           32s

$ kubectl -n buildkit get pods
NAME                     READY   STATUS    RESTARTS   AGE
kube0-6977cdcb75-k9h9m   1/1     Running   0          32s
```

You can use your new builder by including the `--builder` flag when running
buildx commands. For example (replacing `<user>` and `<image>` with your Docker
Hub username and desired image output respectively):

```console
$ docker buildx build . \
  --builder=kube \
  -t <user>/<image> \
  --push
```

## Scaling Buildkit

One of the main advantages of the kubernetes builder is that you can easily
scale your builder up and down to handle increased build load. These controls
are exposed via the following options:

- `replicas=N`
  - This scales the number of buildkit pods to the desired size. By default,
    only a single pod will be created, but increasing this allows taking of
    advantage of multiple nodes in your cluster.
- `requests.cpu`, `requests.memory`, `limits.cpu`, `limits.memory`
  - These options allow requesting and limiting the resources available to each
    buildkit pod according to the official kubernetes documentation
    [here](https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/).
    
For example, to create 4 replica buildkit pods:

```console
$ docker buildx create \
  --bootstrap \
  --name=kube \
  --driver=kubernetes \
  --driver-opt=namespace=buildkit,replicas=4
```

Listing the pods, we get:

```console
$ kubectl -n buildkit get deployments
NAME    READY   UP-TO-DATE   AVAILABLE   AGE
kube0   4/4     4            4           8s

$ kubectl -n buildkit get pods
NAME                     READY   STATUS    RESTARTS   AGE
kube0-6977cdcb75-48ld2   1/1     Running   0          8s
kube0-6977cdcb75-rkc6b   1/1     Running   0          8s
kube0-6977cdcb75-vb4ks   1/1     Running   0          8s
kube0-6977cdcb75-z4fzs   1/1     Running   0          8s
```
    
Additionally, you can use the `loadbalance=(sticky|random)` option to control
the load-balancing behavior when there are multiple replicas. While `random`
should selects random nodes from the available pool, which should provide
better balancing across all replicas, `sticky` (the default) attempts to
connect the same build performed multiple times to the same node each time,
ensuring better local cache utilization.

For more information on scalability, see the options for [buildx create](https://docs.docker.com/engine/reference/commandline/buildx_create/#driver-opt).

## Multi-platform builds

The kubernetes buildx driver has support for creating [multi-platform images](https://docs.docker.com/build/building/multi-platform/),
for easily building for multiple platforms at once.

### QEMU

Like the other containerized driver `docker-container`, the kubernetes driver
also supports using [QEMU](https://www.qemu.org/) (user mode) to build
non-native platforms. If using a default setup like above, no extra setup
should be needed, you should just be able to start building for other
architectures, by including the `--platform` flag.

For example, to build a Linux image for `amd64` and `arm64`:

```console
$ docker buildx build . \
  --builder=kube \
  --platform=linux/amd64,linux/arm64 \
  -t <user>/<image> \
  --push
```

> **Warning**
> QEMU performs full-system emulation of non-native platforms, which is *much*
> slower than native builds. Compute-heavy tasks like compilation and
> compression/decompression will likely take a large performance hit.

Note, if you're using a custom buildkit image using the `image=<image>` driver
option, or invoking non-native binaries from within your build, you may need to
explicitly enable QEMU using the `qemu.install` option during driver creation:

```console
$ docker buildx create \
  --bootstrap \
  --name=kube \
  --driver=kubernetes \
  --driver-opt=namespace=buildkit,qemu.install=true
```

### Native

If you have access to cluster nodes of different architectures, we can
configure the kubernetes driver to take advantage of these for native builds.
To do this, we need to use the `--append` feature of `docker buildx create`.

To start, we can create our builder with explicit support for a single
architecture, `amd64`:

```console
$ docker buildx create \
  --bootstrap \
  --name=kube \
  --driver=kubernetes \
  --platform=linux/amd64 \
  --node=builder-amd64 \
  --driver-opt=namespace=buildkit,nodeselector="kubernetes.io/arch=amd64"
```

This creates a buildx builder `kube` containing a single builder node `builder-amd64`.
Note that the buildx concept of a node is not the same as the kubernetes
concept of a node - the buildx node in this case could connect multiple
kubernetes nodes of the same architecture together.

With our `kube` driver created, we can now introduce another architecture into
the mix, for example, like before we can use `arm64`:

```console
$ docker buildx create \
  --append \
  --bootstrap \
  --name=kube \
  --driver=kubernetes \
  --platform=linux/arm64 \
  --node=builder-arm64 \
  --driver-opt=namespace=buildkit,nodeselector="kubernetes.io/arch=arm64"
```

If you list builders now, you should be able to see both nodes present:

```console
$ docker buildx ls
NAME/NODE       DRIVER/ENDPOINT                                         STATUS   PLATFORMS
kube            kubernetes                                                       
  builder-amd64 kubernetes:///kube?deployment=builder-amd64&kubeconfig= running  linux/amd64*, linux/amd64/v2, linux/amd64/v3, linux/386
  builder-arm64 kubernetes:///kube?deployment=builder-arm64&kubeconfig= running  linux/arm64*
```

You should now be able to build multi-arch images with `amd64` and `arm64`
combined, by specifying those platforms together in your buildx command:

```console
$ docker buildx build --builder=kube --platform=linux/amd64,linux/arm64 -t <user>/<image> --push .
```

You can repeat the `buildx create --append` command for as many different
architectures that you want to support.

## Rootless mode

The kubernetes driver supports rootless mode. For more information on how
rootless mode works, and it's requirements, see [here](https://github.com/moby/buildkit/blob/master/docs/rootless.md).

To enable it in your cluster, you can use the `rootless=true` driver option:

```console
$ docker buildx create \
  --name=kube \
  --driver=kubernetes \
  --driver-opt=namespace=buildkit,rootless=true
```

This will create your pods without `securityContext.privileged`.

## Further reading

For more information on the kubernetes driver, see the [buildx reference](https://docs.docker.com/engine/reference/commandline/buildx_create/#driver).
