# syntax=docker/dockerfile:1

ARG GO_VERSION=1.25
ARG ALPINE_VERSION=3.23
ARG XX_VERSION=1.7.0

# for testing
ARG DOCKER_VERSION=29.0.0
ARG DOCKER_VERSION_ALT_28=28.5
ARG DOCKER_VERSION_ALT_27=27.5.1
ARG DOCKER_CLI_VERSION=${DOCKER_VERSION}
ARG GOTESTSUM_VERSION=v1.13.0
ARG REGISTRY_VERSION=3.0.0
ARG BUILDKIT_VERSION=v0.26.3
ARG COMPOSE_VERSION=v2.39.1
ARG UNDOCK_VERSION=0.9.0

FROM --platform=$BUILDPLATFORM tonistiigi/xx:${XX_VERSION} AS xx
FROM --platform=$BUILDPLATFORM golang:${GO_VERSION}-alpine${ALPINE_VERSION} AS golatest
FROM moby/moby-bin:$DOCKER_VERSION AS docker-engine
FROM dockereng/cli-bin:$DOCKER_CLI_VERSION AS docker-cli
FROM moby/moby-bin:$DOCKER_VERSION_ALT_28 AS docker-engine-alt28
FROM moby/moby-bin:$DOCKER_VERSION_ALT_27 AS docker-engine-alt27
FROM dockereng/cli-bin:$DOCKER_VERSION_ALT_28 AS docker-cli-alt28
FROM dockereng/cli-bin:$DOCKER_VERSION_ALT_27 AS docker-cli-alt27
FROM registry:$REGISTRY_VERSION AS registry
FROM moby/buildkit:$BUILDKIT_VERSION AS buildkit
FROM docker/compose-bin:$COMPOSE_VERSION AS compose
FROM crazymax/undock:$UNDOCK_VERSION AS undock

FROM golatest AS gobase
COPY --from=xx / /
RUN apk add --no-cache file git
ENV GOFLAGS=-mod=vendor
ENV CGO_ENABLED=0
WORKDIR /src

FROM gobase AS gotestsum
ARG GOTESTSUM_VERSION
ENV GOFLAGS=""
RUN --mount=target=/root/.cache,type=cache <<EOT
  set -ex
  go install "gotest.tools/gotestsum@${GOTESTSUM_VERSION}"
  go install "github.com/wadey/gocovmerge@latest"
  mkdir /out
  /go/bin/gotestsum --version
  mv /go/bin/gotestsum /out
  mv /go/bin/gocovmerge /out
EOT
COPY --chmod=755 <<"EOF" /out/gotestsumandcover
#!/bin/sh
set -x
if [ -z "$GO_TEST_COVERPROFILE" ]; then
  exec gotestsum "$@"
fi
coverdir="$(dirname "$GO_TEST_COVERPROFILE")"
mkdir -p "$coverdir/helpers"
gotestsum "$@" "-coverprofile=$GO_TEST_COVERPROFILE"
ecode=$?
go tool covdata textfmt -i=$coverdir/helpers -o=$coverdir/helpers-report.txt
gocovmerge "$coverdir/helpers-report.txt" "$GO_TEST_COVERPROFILE" > "$coverdir/merged-report.txt"
mv "$coverdir/merged-report.txt" "$GO_TEST_COVERPROFILE"
rm "$coverdir/helpers-report.txt"
for f in "$coverdir/helpers"/*; do
  rm "$f"
done
rmdir "$coverdir/helpers"
exit $ecode
EOF

FROM gobase AS buildx-version
RUN --mount=type=bind,target=. <<EOT
  set -e
  PKG=github.com/docker/buildx
  VERSION=$(git describe --match 'v[0-9]*' --dirty='.m' --always --tags)
  REVISION=$(git rev-parse HEAD)$(if ! git diff --no-ext-diff --quiet --exit-code; then echo .m; fi)
  echo "-X ${PKG}/version.Version=${VERSION} -X ${PKG}/version.Revision=${REVISION} -X ${PKG}/version.Package=${PKG}" | tee /tmp/.ldflags
  echo -n "${VERSION}" | tee /tmp/.version
EOT

FROM gobase AS buildx-build
ARG TARGETPLATFORM
ARG GO_EXTRA_FLAGS
RUN --mount=type=bind,target=. \
  --mount=type=cache,target=/root/.cache \
  --mount=type=cache,target=/go/pkg/mod \
  --mount=type=bind,from=buildx-version,source=/tmp/.ldflags,target=/tmp/.ldflags <<EOT
  set -ex
  xx-go build -trimpath ${GO_EXTRA_FLAGS} -ldflags "-s -w $(cat /tmp/.ldflags)" -o /usr/bin/docker-buildx ./cmd/buildx
  file /usr/bin/docker-buildx
  xx-verify --static /usr/bin/docker-buildx
EOT

FROM gobase AS test
ENV SKIP_INTEGRATION_TESTS=1
RUN --mount=type=bind,target=. \
  --mount=type=cache,target=/root/.cache \
  --mount=type=cache,target=/go/pkg/mod \
  go test -v -coverprofile=/tmp/coverage.txt -covermode=atomic ./... && \
  go tool cover -func=/tmp/coverage.txt

FROM scratch AS test-coverage
COPY --from=test /tmp/coverage.txt /coverage.txt

FROM scratch AS binaries-unix
COPY --link --from=buildx-build /usr/bin/docker-buildx /buildx

FROM binaries-unix AS binaries-darwin
FROM binaries-unix AS binaries-freebsd
FROM binaries-unix AS binaries-linux
FROM binaries-unix AS binaries-netbsd
FROM binaries-unix AS binaries-openbsd

FROM scratch AS binaries-windows
COPY --link --from=buildx-build /usr/bin/docker-buildx /buildx.exe

FROM binaries-$TARGETOS AS binaries
# enable scanning for this stage
ARG BUILDKIT_SBOM_SCAN_STAGE=true

FROM gobase AS integration-test-base
# https://github.com/docker/docker/blob/master/project/PACKAGERS.md#runtime-dependencies
RUN apk add --no-cache \
      bash \
      btrfs-progs \
      e2fsprogs \
      e2fsprogs-extra \
      ip6tables \
      iptables \
      make \
      openssl \
      shadow-uidmap \
      xfsprogs \
      xz
COPY --link --from=gotestsum /out /usr/bin/
COPY --link --from=registry /bin/registry /usr/bin/
COPY --link --from=docker-engine / /usr/bin/
COPY --link --from=docker-cli / /usr/bin/
COPY --link --from=docker-engine-alt28 / /opt/docker-alt-28/
COPY --link --from=docker-engine-alt27 / /opt/docker-alt-27/
COPY --link --from=docker-cli-alt28 / /opt/docker-alt-28/
COPY --link --from=docker-cli-alt27 / /opt/docker-alt-27/
COPY --link --from=buildkit /usr/bin/buildkitd /usr/bin/
COPY --link --from=buildkit /usr/bin/buildctl /usr/bin/
COPY --link --from=compose /docker-compose /usr/bin/compose
COPY --link --from=undock /usr/local/bin/undock /usr/bin/
COPY --link --from=binaries /buildx /usr/bin/
RUN mkdir -p /usr/local/lib/docker/cli-plugins && ln -s /usr/bin/buildx /usr/local/lib/docker/cli-plugins/docker-buildx
ENV TEST_DOCKER_EXTRA="docker@28.5=/opt/docker-alt-28,docker@27.5=/opt/docker-alt-27"

FROM integration-test-base AS integration-test
COPY . .

# Release
FROM --platform=$BUILDPLATFORM alpine:${ALPINE_VERSION} AS releaser
WORKDIR /work
ARG TARGETPLATFORM
RUN --mount=from=binaries \
  --mount=type=bind,from=buildx-version,source=/tmp/.version,target=/tmp/.version <<EOT
  set -e
  mkdir -p /out
  cp buildx* "/out/buildx-$(cat /tmp/.version).$(echo $TARGETPLATFORM | sed 's/\//-/g')$(ls buildx* | sed -e 's/^buildx//')"
EOT

FROM scratch AS release
COPY --from=releaser /out/ /

# Shell
FROM docker:$DOCKER_VERSION AS dockerd-release
FROM alpine:${ALPINE_VERSION} AS shell
RUN apk add --no-cache iptables tmux git vim less openssh
RUN mkdir -p /usr/local/lib/docker/cli-plugins && ln -s /usr/local/bin/buildx /usr/local/lib/docker/cli-plugins/docker-buildx
COPY ./hack/demo-env/entrypoint.sh /usr/local/bin
COPY ./hack/demo-env/tmux.conf /root/.tmux.conf
COPY --from=dockerd-release /usr/local/bin /usr/local/bin
WORKDIR /work
COPY ./hack/demo-env/examples .
COPY --from=binaries / /usr/local/bin/
VOLUME /var/lib/docker
ENTRYPOINT ["entrypoint.sh"]

FROM binaries
