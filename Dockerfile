# syntax=docker/dockerfile:1.4

ARG GO_VERSION=1.19
ARG XX_VERSION=1.1.2
ARG DOCKERD_VERSION=20.10.14

FROM docker:$DOCKERD_VERSION AS dockerd-release

# xx is a helper for cross-compilation
FROM --platform=$BUILDPLATFORM tonistiigi/xx:${XX_VERSION} AS xx

FROM --platform=$BUILDPLATFORM golang:${GO_VERSION}-alpine AS golatest

FROM golatest AS gobase
COPY --from=xx / /
RUN apk add --no-cache file git
ENV GOFLAGS=-mod=vendor
ENV CGO_ENABLED=0
WORKDIR /src

FROM gobase AS buildx-version
RUN --mount=type=bind,target=. <<EOT
  set -e
  mkdir /buildx-version
  echo -n "$(./hack/git-meta version)" | tee /buildx-version/version
  echo -n "$(./hack/git-meta revision)" | tee /buildx-version/revision
EOT

FROM gobase AS buildx-build
ARG TARGETPLATFORM
RUN --mount=type=bind,target=. \
  --mount=type=cache,target=/root/.cache \
  --mount=type=cache,target=/go/pkg/mod \
  --mount=type=bind,from=buildx-version,source=/buildx-version,target=/buildx-version <<EOT
  set -e
  xx-go --wrap
  DESTDIR=/usr/bin VERSION=$(cat /buildx-version/version) REVISION=$(cat /buildx-version/revision) GO_EXTRA_LDFLAGS="-s -w" ./hack/build
  xx-verify --static /usr/bin/docker-buildx
EOT

FROM gobase AS test
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
FROM binaries-unix AS binaries-linux

FROM scratch AS binaries-windows
COPY --link --from=buildx-build /usr/bin/docker-buildx /buildx.exe

FROM binaries-$TARGETOS AS binaries

# Release
FROM --platform=$BUILDPLATFORM alpine AS releaser
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
FROM docker:$DOCKERD_VERSION AS dockerd-release
FROM alpine AS shell
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
