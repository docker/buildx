# buildx ls

```
Usage:  docker buildx ls

List builder instances

Options:
      --builder string   Override the configured builder instance
```

## Description

Lists all builder instances and the nodes for each instance

**Example**

```console
$ docker buildx ls

NAME/NODE       DRIVER/ENDPOINT             STATUS  PLATFORMS
elated_tesla *  docker-container
  elated_tesla0 unix:///var/run/docker.sock running linux/amd64
  elated_tesla1 ssh://ubuntu@1.2.3.4        running linux/arm64, linux/arm/v7, linux/arm/v6
default         docker
  default       default                     running linux/amd64
```

Each builder has one or more nodes associated with it. The current builder's
name is marked with a `*`.
