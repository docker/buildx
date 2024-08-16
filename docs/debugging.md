# Debug monitor

To assist with creating and debugging complex builds, Buildx provides a
debugger to help you step through the build process and easily inspect the
state of the build environment at any point.

> [!NOTE]
> The debug monitor is a new experimental feature in recent versions of Buildx.
> There are rough edges, known bugs, and missing features. Please try it out
> and let us know what you think!

## Starting the debugger

To start the debugger, first, ensure that `BUILDX_EXPERIMENTAL=1` is set in
your environment.

```console
$ export BUILDX_EXPERIMENTAL=1
```

To start a debug session for a build, you can use the `buildx debug` command with `--invoke` flag to specify a command to launch in the resulting image.
`buildx debug` command provides `buildx debug build` subcommand that provides the same features as the normal `buildx build` command but allows launching the debugger session after the build.

Arguments available after `buildx debug build` are the same as the normal `buildx build`.

```console
$ docker buildx debug --invoke /bin/sh build .
[+] Building 4.2s (19/19) FINISHED
 => [internal] connecting to local controller                                                                                   0.0s
 => [internal] load build definition from Dockerfile                                                                            0.0s
 => => transferring dockerfile: 32B                                                                                             0.0s
 => [internal] load .dockerignore                                                                                               0.0s
 => => transferring context: 34B                                                                                                0.0s
 ...
Launching interactive container. Press Ctrl-a-c to switch to monitor console
Interactive container was restarted with process "dzz7pjb4pk1mj29xqrx0ac3oj". Press Ctrl-a-c to switch to the new container
Switched IO
/ #
```

This launches a `/bin/sh` process in the final stage of the image, and allows
you to explore the contents of the image, without needing to export or load the
image outside of the builder.

For example, you can use `ls` to see the contents of the image:

```console
/ # ls
bin    etc    lib    mnt    proc   run    srv    tmp    var
dev    home   media  opt    root   sbin   sys    usr    work
```

Optional long form allows you specifying detailed configurations of the process. 
It must be CSV-styled comma-separated key-value pairs.
Supported keys are `args` (can be JSON array format), `entrypoint` (can be JSON array format), `env` (can be JSON array format), `user`, `cwd` and `tty` (bool).

Example:

```
$ docker buildx debug --invoke 'entrypoint=["sh"],"args=[""-c"", ""env | grep -e FOO -e AAA""]","env=[""FOO=bar"", ""AAA=bbb""]"' build .
```

#### `on` flag

If you want to start a debug session when a build fails, you can use
`--on=error` to start a debug session when the build fails.

```console
$ docker buildx debug --on=error build .
[+] Building 4.2s (19/19) FINISHED
 => [internal] connecting to local controller                                                                                   0.0s
 => [internal] load build definition from Dockerfile                                                                            0.0s
 => => transferring dockerfile: 32B                                                                                             0.0s
 => [internal] load .dockerignore                                                                                               0.0s
 => => transferring context: 34B                                                                                                0.0s
 ...
 => ERROR [shell 10/10] RUN bad-command
------
 > [shell 10/10] RUN bad-command:
#0 0.049 /bin/sh: bad-command: not found
------
Launching interactive container. Press Ctrl-a-c to switch to monitor console
Interactive container was restarted with process "edmzor60nrag7rh1mbi4o9lm8". Press Ctrl-a-c to switch to the new container
/ # 
```

This allows you to explore the state of the image when the build failed.

#### Launch the debug session directly with `buildx debug` subcommand

If you want to drop into a debug session without first starting the build, you
can use `buildx debug` command to start a debug session.

```
$ docker buildx debug
[+] Building 4.2s (19/19) FINISHED
 => [internal] connecting to local controller                                                                                   0.0s
(buildx)
```

You can then use the commands available in [monitor mode](#monitor-mode) to
start and observe the build.

## Monitor mode

By default, when debugging, you'll be dropped into a shell in the final stage.

When you're in a debug shell, you can use the `Ctrl-a-c` key combination (press
`Ctrl`+`a` together, lift, then press `c`) to toggle between the debug shell
and the monitor mode. In monitor mode, you can run commands that control the
debug environment.

```console
(buildx) help
Available commands are:
  attach	attach to a buildx server or a process in the container
  disconnect	disconnect a client from a buildx server. Specific session ID can be specified an arg
  exec		execute a process in the interactive container
  exit		exits monitor
  help		shows this message. Optionally pass a command name as an argument to print the detailed usage.
  kill		kill buildx server
  list		list buildx sessions
  ps		list processes invoked by "exec". Use "attach" to attach IO to that process
  reload	reloads the context and build it
  rollback	re-runs the interactive container with the step's rootfs contents
```

## Build controllers

Debugging is performed using a buildx "controller", which provides a high-level
abstraction to perform builds. By default, the local controller is used for a
more stable experience which runs all builds in-process. However, you can also
use the remote controller to detach the build process from the CLI.

To detach the build process from the CLI, you can use the `--detach=true` flag with
the build command.

```console
$ docker buildx debug --invoke /bin/sh build --detach=true .
```

If you start a debugging session using the `--invoke` flag with a detached
build, then you can attach to it using the `buildx debug` command to
immediately enter the monitor mode.

```console
$ docker buildx debug
[+] Building 0.0s (1/1) FINISHED                                                                                                                                                                                
 => [internal] connecting to remote controller
(buildx) list
ID                              CURRENT_SESSION
xfe1162ovd9def8yapb4ys66t       false
(buildx) attach xfe1162ovd9def8yapb4ys66t
Attached to process "". Press Ctrl-a-c to switch to the new container
(buildx) ps
PID                             CURRENT_SESSION COMMAND
3ug8iqaufiwwnukimhqqt06jz       false           [sh]
(buildx) attach 3ug8iqaufiwwnukimhqqt06jz
Attached to process "3ug8iqaufiwwnukimhqqt06jz". Press Ctrl-a-c to switch to the new container
(buildx) Switched IO
/ # ls
bin    etc    lib    mnt    proc   run    srv    tmp    var
dev    home   media  opt    root   sbin   sys    usr    work
/ # 
```
