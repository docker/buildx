# syntax=docker/dockerfile:1

ARG GO_VERSION=1.23
ARG ALPINE_VERSION=3.21
ARG XX_VERSION=1.6.1

# for testing
ARG DOCKER_VERSION=27.5.0
ARG DOCKER_VERSION_ALT_26=26.1.3
ARG DOCKER_CLI_VERSION=${DOCKER_VERSION}
ARG GOTESTSUM_VERSION=v1.12.0
ARG REGISTRY_VERSION=2.8.3
ARG BUILDKIT_VERSION=v0.19.0-rc2
ARG UNDOCK_VERSION=0.9.0

FROM --platform=$BUILDPLATFORM tonistiigi/xx:${XX_VERSION} AS xx
FROM --platform=$BUILDPLATFORM golang:${GO_VERSION}-alpine${ALPINE_VERSION} AS golatest
FROM moby/moby-bin:$DOCKER_VERSION AS docker-engine
FROM dockereng/cli-bin:$DOCKER_CLI_VERSION AS docker-cli
FROM moby/moby-bin:$DOCKER_VERSION_ALT_26 AS docker-engine-alt
FROM dockereng/cli-bin:$DOCKER_VERSION_ALT_26 AS docker-cli-alt
FROM registry:$REGISTRY_VERSION AS registry
FROM moby/buildkit:$BUILDKIT_VERSION AS buildkit
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
  mkdir /buildx-version
  echo -n "$(./hack/git-meta version)" | tee /buildx-version/version
  echo -n "$(./hack/git-meta revision)" | tee /buildx-version/revision
EOT

FROM gobase AS buildx-build
ARG TARGETPLATFORM
ARG GO_EXTRA_FLAGS
RUN --mount=type=bind,target=. \
  --mount=type=cache,target=/root/.cache \
  --mount=type=cache,target=/go/pkg/mod \
  --mount=type=bind,from=buildx-version,source=/buildx-version,target=/buildx-version <<EOT
  set -e
  xx-go --wrap
  DESTDIR=/usr/bin VERSION=$(cat /buildx-version/version) REVISION=$(cat /buildx-version/revision) GO_EXTRA_LDFLAGS="-s -w" ./hack/build
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
FROM binaries-unix AS binaries-openbsd

FROM scratch AS binaries-windows
COPY --link --from=buildx-build /usr/bin/docker-buildx /buildx.exe

FROM binaries-$TARGETOS AS binaries
# enable scanning for this stage
ARG BUILDKIT_SBOM_SCAN_STAGE=true

FROM gobase AS integration-test-base
# https://github.com/docker/docker/blob/master/project/PACKAGERS.md#runtime-dependencies
RUN apk add --no-cache \
      btrfs-progs \
      e2fsprogs \
      e2fsprogs-extra \
      ip6tables \
      iptables \
      openssl \
      shadow-uidmap \
      xfsprogs \
      xz
COPY --link --from=gotestsum /out /usr/bin/
COPY --link --from=registry /bin/registry /usr/bin/
COPY --link --from=docker-engine / /usr/bin/
COPY --link --from=docker-cli / /usr/bin/
COPY --link --from=docker-engine-alt / /opt/docker-alt-26/
COPY --link --from=docker-cli-alt / /opt/docker-alt-26/
COPY --link --from=buildkit /usr/bin/buildkitd /usr/bin/
COPY --link --from=buildkit /usr/bin/buildctl /usr/bin/
COPY --link --from=undock /usr/local/bin/undock /usr/bin/
COPY --link --from=binaries /buildx /usr/bin/
ENV TEST_DOCKER_EXTRA="docker@26.1=/opt/docker-alt-26"

FROM integration-test-base AS integration-test
COPY . .

# Release
FROM --platform=$BUILDPLATFORM alpine:${ALPINE_VERSION} AS releaser
WORKDIR /work
ARG TARGETPLATFORM
RUN --mount=from=binaries \
  --mount=type=bind,from=buildx-version,source=/buildx-version,target=/buildx-version <<EOT
  set -e
  mkdir -p /out
  cp buildx* "/out/buildx-$(cat /buildx-version/version).$(echo $TARGETPLATFORM | sed 's/\//-/g')$(ls buildx* | sed -e 's/^buildx//')"
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
