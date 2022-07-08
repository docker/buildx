---
title: "Remote builder"
description: "Connect buildx to an external buildkitd instance"
keywords: build, buildx, buildkit
---

The buildx remote driver allows for more complex custom build workloads that
allow users to connect to external buildkit instances. This is useful for
scenarios that require manual management of the buildkit daemon, or where a
buildkit daemon is exposed from another source.

To connect to a running buildkitd instance:

```console
$ docker buildx create \
  --name remote \
  --driver remote \
  tcp://localhost:1234
```

## Remote Buildkit over Unix sockets

In this scenario, we'll create a setup with buildkitd listening on a unix
socket, and have buildx connect through it.

Firstly, ensure that [buildkit](https://github.com/moby/buildkit) is installed.
For example, you can launch an instance of buildkitd with:

```console
$ sudo ./buildkitd --group $(id -gn) --addr unix://$HOME/buildkitd.sock
```

Alternatively, [see here](https://github.com/moby/buildkit/blob/master/docs/rootless.md)
for running buildkitd in rootless mode or [here](https://github.com/moby/buildkit/tree/master/examples/systemd)
for examples of running it as a systemd service.

You should now have a unix socket accessible to your user, that is available to
connect to:

```console
$ ls -lh /home/user/buildkitd.sock
srw-rw---- 1 root user 0 May  5 11:04 /home/user/buildkitd.sock
```

You can then connect buildx to it with the remote driver:

```console
$ docker buildx create \
  --name remote-unix \
  --driver remote \
  unix://$HOME/buildkitd.sock
```
        
If you list available builders, you should then see `remote-unix` among them:

```console
$ docker buildx ls
NAME/NODE           DRIVER/ENDPOINT                        STATUS  PLATFORMS
remote-unix         remote
  remote-unix0      unix:///home/.../buildkitd.sock        running linux/amd64, linux/amd64/v2, linux/amd64/v3, linux/386
default *           docker
  default           default                                running linux/amd64, linux/386
```

We can switch to this new builder as the default using `docker buildx use remote-unix`,
or specify it per build:

```console
$ docker buildx build --builder=remote-unix -t test --load .
```

(remember that `--load` is necessary when not using the default `docker`
driver, to load the build result into the docker daemon)

## Remote Buildkit in Docker container

In this scenario, we'll create a similar setup to the `docker-container`
driver, by manually booting a buildkit docker container and connecting to it
using the buildx remote driver. In most cases you'd probably just use the
`docker-container` driver that connects to buildkit through the Docker daemon,
but in this case we manually create a container and access it via it's exposed
port.

First, we need to generate certificates for buildkit - you can use the 
[create-certs.sh](https://github.com/moby/buildkit/v0.10.3/master/examples/kubernetes/create-certs.sh)
script as a starting point. Note, that while it is *possible* to expose
buildkit over TCP without using TLS, it is **not recommended**, since this will
allow arbitrary access to buildkit without credentials.

With our certificates generated in `.certs/`, we startup the container:

```console
$ docker run -d --rm \
  --name=remote-buildkitd \
  --privileged \
  -p 1234:1234 \
  -v $PWD/.certs:/etc/buildkit/certs \
  moby/buildkit:latest \
  --addr tcp://0.0.0.0:1234 \
  --tlscacert /etc/buildkit/certs/ca.pem \
  --tlscert /etc/buildkit/certs/daemon-cert.pem \
  --tlskey /etc/buildkit/certs/daemon-key.pem
```

The above command starts a buildkit container and exposes the daemon's port
1234 to localhost.

We can now connect to this running container using buildx:

```console
$ docker buildx create \
  --name remote-container \
  --driver remote \
  --driver-opt cacert=.certs/ca.pem,cert=.certs/client-cert.pem,key=.certs/client-key.pem,servername=... \
  tcp://localhost:1234
```

Alternatively, we could use the `docker-container://` URL scheme to connect
to the buildkit container without specifying a port:

```console
$ docker buildx create \
  --name remote-container \
  --driver remote \
  docker-container://remote-container
```

## Remote Buildkit in Kubernetes

In this scenario, we'll create a similar setup to the `kubernetes` driver by
manually creating a buildkit `Deployment`. While the `kubernetes` driver will
do this under-the-hood, it might sometimes be desirable to scale buildkit
manually. Additionally, when executing builds from inside Kubernetes pods,
the buildx builder will need to be recreated from within each pod or copied
between them.

Firstly, we can create a kubernetes deployment of buildkitd, as per the
instructions [here](https://github.com/moby/buildkit/tree/master/examples/kubernetes).
Following the guide, we setup certificates for the buildkit daemon and client
(as above using [create-certs.sh](https://github.com/moby/buildkit/blob/v0.10.3/examples/kubernetes/create-certs.sh))
and create a `Deployment` of buildkit pods with a service that connects to
them.

Assuming that the service is called `buildkitd`, we can create a remote builder
in buildx, ensuring that the listed certificate files are present:

```console
$ docker buildx create \
  --name remote-kubernetes \
  --driver remote \
  --driver-opt cacert=.certs/ca.pem,cert=.certs/client-cert.pem,key=.certs/client-key.pem \
  tcp://buildkitd.default.svc:1234
```

Note that the above will only work in-cluster (since the buildkit setup guide
only creates a ClusterIP service). To configure the builder to be accessible
remotely, you can use an appropriately configured Ingress, which is outside the
scope of this guide.

To access the service remotely, we can use the port forwarding mechanism in
kubectl:

```console
$ kubectl port-forward svc/buildkitd 1234:1234
```

Then you can simply point the remote driver at `tcp://localhost:1234`.

Alternatively, we could use the `kube-pod://` URL scheme to connect
directly to a buildkit pod through the kubernetes api (note that this method
will only connect to a single pod in the deployment):

```console
$ kubectl get pods --selector=app=buildkitd -o json | jq -r '.items[].metadata.name
buildkitd-XXXXXXXXXX-xxxxx
$ docker buildx create \
  --name remote-container \
  --driver remote \
  kube-pod://buildkitd-XXXXXXXXXX-xxxxx
```
