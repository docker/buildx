# buildx imagetools inspect

```
docker buildx imagetools inspect [OPTIONS] NAME
```

<!---MARKER_GEN_START-->
Show details of an image in the registry

### Options

| Name | Type | Default | Description |
| --- | --- | --- | --- |
| [`--builder`](#builder) | `string` |  | Override the configured builder instance |
| [`--raw`](#raw) |  |  | Show original JSON manifest |


<!---MARKER_GEN_END-->

## Description

Show details of an image in the registry.

## Examples

### <a name="builder"></a> Override the configured builder instance (--builder)

Same as [`buildx --builder`](buildx.md#builder).

### <a name="format"></a> Format the output (--format)

Format the output using the given Go template. At the moment the following
fields are available:

* `.Name`: provides the reference of the image
* `.Manifest`: provides manifest or manifest list
* `.Config`: provides the image config as `application/vnd.oci.image.config.v1+json` mediatype
* `.BuildInfo`: provides [build info from image config](https://github.com/moby/buildkit/blob/master/docs/build-repro.md#image-config)

#### `.Name`

```console
$ docker buildx imagetools inspect alpine --format "{{.Name}}"
Name: docker.io/library/alpine:latest
```

#### `.Manifest`

```console
$ docker buildx imagetools inspect crazymax/loop --format "{{.Manifest}}"
Name:      docker.io/crazymax/loop:latest
MediaType: application/vnd.docker.distribution.manifest.v2+json
Digest:    sha256:08602e7340970e92bde5e0a2e887c1fde4d9ae753d1e05efb4c8ef3b609f97f1
```

```console
$ docker buildx imagetools inspect moby/buildkit:master --format "{{.Manifest}}"
Name:      docker.io/moby/buildkit:master
MediaType: application/vnd.docker.distribution.manifest.list.v2+json
Digest:    sha256:4e078bb87be98cc2a0fcdac4a05ac934055d563bd3d23e4d714eb03a3f62b49e

Manifests:
  Name:      docker.io/moby/buildkit:master@sha256:bd1e78f06de26610fadf4eb9d04b1a45a545799d6342701726e952cc0c11c912
  MediaType: application/vnd.docker.distribution.manifest.v2+json
  Platform:  linux/amd64

  Name:      docker.io/moby/buildkit:master@sha256:d37dcced63ec0965824fca644f0ac9efad8569434ec15b4c83adfcb3dcfc743b
  MediaType: application/vnd.docker.distribution.manifest.v2+json
  Platform:  linux/arm/v7

  Name:      docker.io/moby/buildkit:master@sha256:ce142eb2255e6af46f2809e159fd03081697c7605a3de03b9cbe9a52ddb244bf
  MediaType: application/vnd.docker.distribution.manifest.v2+json
  Platform:  linux/arm64

  Name:      docker.io/moby/buildkit:master@sha256:f59bfb5062fff76ce464bfa4e25ebaaaac887d6818238e119d68613c456d360c
  MediaType: application/vnd.docker.distribution.manifest.v2+json
  Platform:  linux/s390x

  Name:      docker.io/moby/buildkit:master@sha256:cc96426e0c50a78105d5637d31356db5dd6ec594f21b24276e534a32da09645c
  MediaType: application/vnd.docker.distribution.manifest.v2+json
  Platform:  linux/ppc64le

  Name:      docker.io/moby/buildkit:master@sha256:39f9c1e2878e6c333acb23187d6b205ce82ed934c60da326cb2c698192631478
  MediaType: application/vnd.docker.distribution.manifest.v2+json
  Platform:  linux/riscv64
```

#### `.BuildInfo`

```console
$ docker buildx imagetools inspect crazymax/buildx:buildinfo --format "{{.BuildInfo}}"
Name: docker.io/crazymax/buildx:buildinfo
Frontend: dockerfile.v0
Attrs:
  build-arg:bar: foo
  build-arg:foo: bar
  filename:      Dockerfile
  source:        crazymax/dockerfile:buildattrs
Sources:
  Type: docker-image
  Ref:  docker.io/docker/buildx-bin:0.6.1@sha256:a652ced4a4141977c7daaed0a074dcd9844a78d7d2615465b12f433ae6dd29f0
  Pin:  sha256:a652ced4a4141977c7daaed0a074dcd9844a78d7d2615465b12f433ae6dd29f0

  Type: docker-image
  Ref:  docker.io/library/alpine:3.13@sha256:026f721af4cf2843e07bba648e158fb35ecc876d822130633cc49f707f0fc88c
  Pin:  sha256:026f721af4cf2843e07bba648e158fb35ecc876d822130633cc49f707f0fc88c

  Type: docker-image
  Ref:  docker.io/moby/buildkit:v0.9.0@sha256:8dc668e7f66db1c044aadbed306020743516a94848793e0f81f94a087ee78cab
  Pin:  sha256:8dc668e7f66db1c044aadbed306020743516a94848793e0f81f94a087ee78cab

  Type: docker-image
  Ref:  docker.io/tonistiigi/xx@sha256:21a61be4744f6531cb5f33b0e6f40ede41fa3a1b8c82d5946178f80cc84bfc04
  Pin:  sha256:21a61be4744f6531cb5f33b0e6f40ede41fa3a1b8c82d5946178f80cc84bfc04

  Type: http
  Ref:  https://raw.githubusercontent.com/moby/moby/master/README.md
  Pin:  sha256:419455202b0ef97e480d7f8199b26a721a417818bc0e2d106975f74323f25e6c
```

#### JSON output

A `json` go template func is also available if you want to render fields as
JSON bytes:

```console
$ docker buildx imagetools inspect crazymax/loop --format "{{json .Manifest}}"
```
```json
{
  "mediaType": "application/vnd.docker.distribution.manifest.v2+json",
  "digest": "sha256:08602e7340970e92bde5e0a2e887c1fde4d9ae753d1e05efb4c8ef3b609f97f1",
  "size": 949
}
```

```console
$ docker buildx imagetools inspect moby/buildkit:master --format "{{json .Manifest}}"
```
```json
{
  "schemaVersion": 2,
  "mediaType": "application/vnd.docker.distribution.manifest.list.v2+json",
  "manifests": [
    {
      "mediaType": "application/vnd.docker.distribution.manifest.v2+json",
      "digest": "sha256:bd1e78f06de26610fadf4eb9d04b1a45a545799d6342701726e952cc0c11c912",
      "size": 1158,
      "platform": {
        "architecture": "amd64",
        "os": "linux"
      }
    },
    {
      "mediaType": "application/vnd.docker.distribution.manifest.v2+json",
      "digest": "sha256:d37dcced63ec0965824fca644f0ac9efad8569434ec15b4c83adfcb3dcfc743b",
      "size": 1158,
      "platform": {
        "architecture": "arm",
        "os": "linux",
        "variant": "v7"
      }
    },
    {
      "mediaType": "application/vnd.docker.distribution.manifest.v2+json",
      "digest": "sha256:ce142eb2255e6af46f2809e159fd03081697c7605a3de03b9cbe9a52ddb244bf",
      "size": 1158,
      "platform": {
        "architecture": "arm64",
        "os": "linux"
      }
    },
    {
      "mediaType": "application/vnd.docker.distribution.manifest.v2+json",
      "digest": "sha256:f59bfb5062fff76ce464bfa4e25ebaaaac887d6818238e119d68613c456d360c",
      "size": 1158,
      "platform": {
        "architecture": "s390x",
        "os": "linux"
      }
    },
    {
      "mediaType": "application/vnd.docker.distribution.manifest.v2+json",
      "digest": "sha256:cc96426e0c50a78105d5637d31356db5dd6ec594f21b24276e534a32da09645c",
      "size": 1159,
      "platform": {
        "architecture": "ppc64le",
        "os": "linux"
      }
    },
    {
      "mediaType": "application/vnd.docker.distribution.manifest.v2+json",
      "digest": "sha256:39f9c1e2878e6c333acb23187d6b205ce82ed934c60da326cb2c698192631478",
      "size": 1158,
      "platform": {
        "architecture": "riscv64",
        "os": "linux"
      }
    }
  ]
}
```

```console
$ docker buildx imagetools inspect crazymax/buildx:buildinfo --format "{{json .BuildInfo}}"
```
```json
{
  "frontend": "dockerfile.v0",
  "attrs": {
    "build-arg:bar": "foo",
    "build-arg:foo": "bar",
    "filename": "Dockerfile",
    "source": "crazymax/dockerfile:buildattrs"
  },
  "sources": [
    {
      "type": "docker-image",
      "ref": "docker.io/docker/buildx-bin:0.6.1@sha256:a652ced4a4141977c7daaed0a074dcd9844a78d7d2615465b12f433ae6dd29f0",
      "pin": "sha256:a652ced4a4141977c7daaed0a074dcd9844a78d7d2615465b12f433ae6dd29f0"
    },
    {
      "type": "docker-image",
      "ref": "docker.io/library/alpine:3.13@sha256:026f721af4cf2843e07bba648e158fb35ecc876d822130633cc49f707f0fc88c",
      "pin": "sha256:026f721af4cf2843e07bba648e158fb35ecc876d822130633cc49f707f0fc88c"
    },
    {
      "type": "docker-image",
      "ref": "docker.io/moby/buildkit:v0.9.0@sha256:8dc668e7f66db1c044aadbed306020743516a94848793e0f81f94a087ee78cab",
      "pin": "sha256:8dc668e7f66db1c044aadbed306020743516a94848793e0f81f94a087ee78cab"
    },
    {
      "type": "docker-image",
      "ref": "docker.io/tonistiigi/xx@sha256:21a61be4744f6531cb5f33b0e6f40ede41fa3a1b8c82d5946178f80cc84bfc04",
      "pin": "sha256:21a61be4744f6531cb5f33b0e6f40ede41fa3a1b8c82d5946178f80cc84bfc04"
    },
    {
      "type": "http",
      "ref": "https://raw.githubusercontent.com/moby/moby/master/README.md",
      "pin": "sha256:419455202b0ef97e480d7f8199b26a721a417818bc0e2d106975f74323f25e6c"
    }
  ]
}
```

```console
$ docker buildx imagetools inspect crazymax/buildx:buildinfo --format "{{json .}}"
```
```json
{
  "name": "crazymax/buildx:buildinfo",
  "manifest": {
    "mediaType": "application/vnd.docker.distribution.manifest.v2+json",
    "digest": "sha256:ac98300a6580fa30d9350a868de2a09f5a0190f4ba94e6c0ead8cc892150977c",
    "size": 2628
  },
  "config": {
    "created": "2022-02-24T12:27:43.627154558Z",
    "architecture": "amd64",
    "os": "linux",
    "config": {
      "Env": [
        "PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
        "DOCKER_TLS_CERTDIR=/certs",
        "DOCKER_CLI_EXPERIMENTAL=enabled"
      ],
      "Entrypoint": [
        "docker-entrypoint.sh"
      ],
      "Cmd": [
        "sh"
      ]
    },
    "rootfs": {
      "type": "layers",
      "diff_ids": [
        "sha256:7fcb75871b2101082203959c83514ac8a9f4ecfee77a0fe9aa73bbe56afdf1b4",
        "sha256:d3c0b963ff5684160641f936d6a4aa14efc8ff27b6edac255c07f2d03ff92e82",
        "sha256:3f8d78f13fa9b1f35d3bc3f1351d03a027c38018c37baca73f93eecdea17f244",
        "sha256:8e6eb1137b182ae0c3f5d40ca46341fda2eaeeeb5fa516a9a2bf96171238e2e0",
        "sha256:fde4c869a56b54dd76d7352ddaa813fd96202bda30b9dceb2c2f2ad22fa2e6ce",
        "sha256:52025823edb284321af7846419899234b3c66219bf06061692b709875ed0760f",
        "sha256:50adb5982dbf6126c7cf279ac3181d1e39fc9116b610b947a3dadae6f7e7c5bc",
        "sha256:9801c319e1c66c5d295e78b2d3e80547e73c7e3c63a4b71e97c8ca357224af24",
        "sha256:dfbfac44d5d228c49b42194c8a2f470abd6916d072f612a6fb14318e94fde8ae",
        "sha256:3dfb74e19dedf61568b917c19b0fd3ee4580870027ca0b6054baf239855d1322",
        "sha256:b182e707c23e4f19be73f9022a99d2d1ca7bf1ca8f280d40e4d1c10a6f51550e"
      ]
    },
    "history": [
      {
        "created": "2021-11-12T17:19:58.698676655Z",
        "created_by": "/bin/sh -c #(nop) ADD file:5a707b9d6cb5fff532e4c2141bc35707593f21da5528c9e71ae2ddb6ba4a4eb6 in / "
      },
      {
        "created": "2021-11-12T17:19:58.948920855Z",
        "created_by": "/bin/sh -c #(nop)  CMD [\"/bin/sh\"]",
        "empty_layer": true
      },
      {
        "created": "2022-02-24T12:27:38.285594601Z",
        "created_by": "RUN /bin/sh -c apk --update --no-cache add     bash     ca-certificates     openssh-client   \u0026\u0026 rm -rf /tmp/* /var/cache/apk/* # buildkit",
        "comment": "buildkit.dockerfile.v0"
      },
      {
        "created": "2022-02-24T12:27:41.061874167Z",
        "created_by": "COPY /opt/docker/ /usr/local/bin/ # buildkit",
        "comment": "buildkit.dockerfile.v0"
      },
      {
        "created": "2022-02-24T12:27:41.174098947Z",
        "created_by": "COPY /usr/bin/buildctl /usr/local/bin/buildctl # buildkit",
        "comment": "buildkit.dockerfile.v0"
      },
      {
        "created": "2022-02-24T12:27:41.320343683Z",
        "created_by": "COPY /usr/bin/buildkit* /usr/local/bin/ # buildkit",
        "comment": "buildkit.dockerfile.v0"
      },
      {
        "created": "2022-02-24T12:27:41.447149933Z",
        "created_by": "COPY /buildx /usr/libexec/docker/cli-plugins/docker-buildx # buildkit",
        "comment": "buildkit.dockerfile.v0"
      },
      {
        "created": "2022-02-24T12:27:43.057722191Z",
        "created_by": "COPY /opt/docker-compose /usr/libexec/docker/cli-plugins/docker-compose # buildkit",
        "comment": "buildkit.dockerfile.v0"
      },
      {
        "created": "2022-02-24T12:27:43.145224134Z",
        "created_by": "ADD https://raw.githubusercontent.com/moby/moby/master/README.md / # buildkit",
        "comment": "buildkit.dockerfile.v0"
      },
      {
        "created": "2022-02-24T12:27:43.422212427Z",
        "created_by": "ENV DOCKER_TLS_CERTDIR=/certs",
        "comment": "buildkit.dockerfile.v0",
        "empty_layer": true
      },
      {
        "created": "2022-02-24T12:27:43.422212427Z",
        "created_by": "ENV DOCKER_CLI_EXPERIMENTAL=enabled",
        "comment": "buildkit.dockerfile.v0",
        "empty_layer": true
      },
      {
        "created": "2022-02-24T12:27:43.422212427Z",
        "created_by": "RUN /bin/sh -c docker --version   \u0026\u0026 buildkitd --version   \u0026\u0026 buildctl --version   \u0026\u0026 docker buildx version   \u0026\u0026 docker compose version   \u0026\u0026 mkdir /certs /certs/client   \u0026\u0026 chmod 1777 /certs /certs/client # buildkit",
        "comment": "buildkit.dockerfile.v0"
      },
      {
        "created": "2022-02-24T12:27:43.514320155Z",
        "created_by": "COPY rootfs/modprobe.sh /usr/local/bin/modprobe # buildkit",
        "comment": "buildkit.dockerfile.v0"
      },
      {
        "created": "2022-02-24T12:27:43.627154558Z",
        "created_by": "COPY rootfs/docker-entrypoint.sh /usr/local/bin/ # buildkit",
        "comment": "buildkit.dockerfile.v0"
      },
      {
        "created": "2022-02-24T12:27:43.627154558Z",
        "created_by": "ENTRYPOINT [\"docker-entrypoint.sh\"]",
        "comment": "buildkit.dockerfile.v0",
        "empty_layer": true
      },
      {
        "created": "2022-02-24T12:27:43.627154558Z",
        "created_by": "CMD [\"sh\"]",
        "comment": "buildkit.dockerfile.v0",
        "empty_layer": true
      }
    ]
  },
  "buildinfo": {
    "frontend": "dockerfile.v0",
    "attrs": {
      "build-arg:bar": "foo",
      "build-arg:foo": "bar",
      "filename": "Dockerfile",
      "source": "crazymax/dockerfile:buildattrs"
    },
    "sources": [
      {
        "type": "docker-image",
        "ref": "docker.io/docker/buildx-bin:0.6.1@sha256:a652ced4a4141977c7daaed0a074dcd9844a78d7d2615465b12f433ae6dd29f0",
        "pin": "sha256:a652ced4a4141977c7daaed0a074dcd9844a78d7d2615465b12f433ae6dd29f0"
      },
      {
        "type": "docker-image",
        "ref": "docker.io/library/alpine:3.13@sha256:026f721af4cf2843e07bba648e158fb35ecc876d822130633cc49f707f0fc88c",
        "pin": "sha256:026f721af4cf2843e07bba648e158fb35ecc876d822130633cc49f707f0fc88c"
      },
      {
        "type": "docker-image",
        "ref": "docker.io/moby/buildkit:v0.9.0@sha256:8dc668e7f66db1c044aadbed306020743516a94848793e0f81f94a087ee78cab",
        "pin": "sha256:8dc668e7f66db1c044aadbed306020743516a94848793e0f81f94a087ee78cab"
      },
      {
        "type": "docker-image",
        "ref": "docker.io/tonistiigi/xx@sha256:21a61be4744f6531cb5f33b0e6f40ede41fa3a1b8c82d5946178f80cc84bfc04",
        "pin": "sha256:21a61be4744f6531cb5f33b0e6f40ede41fa3a1b8c82d5946178f80cc84bfc04"
      },
      {
        "type": "http",
        "ref": "https://raw.githubusercontent.com/moby/moby/master/README.md",
        "pin": "sha256:419455202b0ef97e480d7f8199b26a721a417818bc0e2d106975f74323f25e6c"
      }
    ]
  }
}
```

### <a name="raw"></a> Show original, unformatted JSON manifest (--raw)

Use the `--raw` option to print the unformatted JSON manifest bytes.

> `jq` is used here to get a better rendering of the output result.

```console
$ docker buildx imagetools inspect --raw crazymax/loop | jq
```
```json
{
  "mediaType": "application/vnd.docker.distribution.manifest.v2+json",
  "schemaVersion": 2,
  "config": {
    "mediaType": "application/vnd.docker.container.image.v1+json",
    "digest": "sha256:7ace7d324e79b360b2db8b820d83081863d96d22e734cdf297a8e7fd83f6ceb3",
    "size": 2298
  },
  "layers": [
    {
      "mediaType": "application/vnd.docker.image.rootfs.diff.tar.gzip",
      "digest": "sha256:5843afab387455b37944e709ee8c78d7520df80f8d01cf7f861aae63beeddb6b",
      "size": 2811478
    },
    {
      "mediaType": "application/vnd.docker.image.rootfs.diff.tar.gzip",
      "digest": "sha256:726d3732a87e1c430d67e8969de6b222a889d45e045ebae1a008a37ba38f3b1f",
      "size": 1776812
    },
    {
      "mediaType": "application/vnd.docker.image.rootfs.diff.tar.gzip",
      "digest": "sha256:5d7cf9b33148a8f220c84f27dd2cfae46aca019a3ea3fbf7274f6d6dbfae8f3b",
      "size": 382855
    }
  ]
}
```

```console
$ docker buildx imagetools inspect --raw moby/buildkit:master | jq
```
```json
{
  "mediaType": "application/vnd.docker.distribution.manifest.list.v2+json",
  "schemaVersion": 2,
  "manifests": [
    {
      "mediaType": "application/vnd.docker.distribution.manifest.v2+json",
      "digest": "sha256:905307ef07e366e5977163218b9f01e9553fe2c15fe5e4a529328f91b510351d",
      "size": 1158,
      "platform": {
        "architecture": "amd64",
        "os": "linux"
      }
    },
    {
      "mediaType": "application/vnd.docker.distribution.manifest.v2+json",
      "digest": "sha256:250a6dd96c377a1084cf8ea766916ae643f2fdf0f2e68728b3fda0a4d4669a2e",
      "size": 1158,
      "platform": {
        "architecture": "arm",
        "os": "linux",
        "variant": "v7"
      }
    },
    {
      "mediaType": "application/vnd.docker.distribution.manifest.v2+json",
      "digest": "sha256:7ee37cac4b2b8b54d28127a10aa783260430a145bafab169cec88b8a20462678",
      "size": 1158,
      "platform": {
        "architecture": "arm64",
        "os": "linux"
      }
    },
    {
      "mediaType": "application/vnd.docker.distribution.manifest.v2+json",
      "digest": "sha256:0ba8d091f6b346a5c45b06dcb534b6b6945a681fcdf40c195e1d6619115dffd2",
      "size": 1158,
      "platform": {
        "architecture": "s390x",
        "os": "linux"
      }
    },
    {
      "mediaType": "application/vnd.docker.distribution.manifest.v2+json",
      "digest": "sha256:964d4b79d1be97ce22e6fe0c29be1d6cc63da10e5b8a21505851014c9846268a",
      "size": 1159,
      "platform": {
        "architecture": "ppc64le",
        "os": "linux"
      }
    },
    {
      "mediaType": "application/vnd.docker.distribution.manifest.v2+json",
      "digest": "sha256:44d8de2c5f813b48d649a3a6cc348b57387cc22355897a14c7447cbfa03d079c",
      "size": 1158,
      "platform": {
        "architecture": "riscv64",
        "os": "linux"
      }
    }
  ]
}
```
