# syntax=docker/dockerfile:1.4

ARG GO_VERSION=1.18
ARG DOCKERD_VERSION=20.10.14

FROM docker:$DOCKERD_VERSION AS dockerd-release

# xx is a helper for cross-compilation
FROM --platform=$BUILDPLATFORM tonistiigi/xx:1.1.0 AS xx

FROM --platform=$BUILDPLATFORM golang:${GO_VERSION}-alpine AS golatest

FROM golatest AS gobase
COPY --from=xx / /
RUN apk add --no-cache file git
ENV GOFLAGS=-mod=vendor
WORKDIR /src

FROM gobase AS buildx-version
RUN --mount=target=. \
  PKG=github.com/docker/buildx VERSION=$(git describe --match 'v[0-9]*' --dirty='.m' --always --tags) REVISION=$(git rev-parse HEAD)$(if ! git diff --no-ext-diff --quiet --exit-code; then echo .m; fi); \
  echo "-X ${PKG}/version.Version=${VERSION} -X ${PKG}/version.Revision=${REVISION} -X ${PKG}/version.Package=${PKG}" | tee /tmp/.ldflags; \
  echo -n "${VERSION}" | tee /tmp/.version;

FROM gobase AS buildx-build
ENV CGO_ENABLED=0
ARG LDFLAGS="-w -s"
ARG TARGETPLATFORM
RUN --mount=type=bind,target=. \
  --mount=type=cache,target=/root/.cache \
  --mount=type=cache,target=/go/pkg/mod \
  --mount=type=bind,source=/tmp/.ldflags,target=/tmp/.ldflags,from=buildx-version \
  set -x; xx-go build -ldflags "$(cat /tmp/.ldflags) ${LDFLAGS}" -o /usr/bin/buildx ./cmd/buildx && \
  xx-verify --static /usr/bin/buildx

FROM buildx-build AS test
RUN --mount=type=bind,target=. \
  --mount=type=cache,target=/root/.cache \
  --mount=type=cache,target=/go/pkg/mod \
  go test -v -coverprofile=/tmp/coverage.txt -covermode=atomic ./... && \
  go tool cover -func=/tmp/coverage.txt

FROM scratch AS test-coverage
COPY --from=test /tmp/coverage.txt /coverage.txt

FROM scratch AS binaries-unix
COPY --link --from=buildx-build /usr/bin/buildx /

FROM binaries-unix AS binaries-darwin
FROM binaries-unix AS binaries-linux

FROM scratch AS binaries-windows
COPY --link --from=buildx-build /usr/bin/buildx /buildx.exe

FROM binaries-$TARGETOS AS binaries

# Release
FROM --platform=$BUILDPLATFORM alpine AS releaser
WORKDIR /work
ARG TARGETPLATFORM
RUN --mount=from=binaries \
  --mount=type=bind,source=/tmp/.version,target=/tmp/.version,from=buildx-version \
  mkdir -p /out && cp buildx* "/out/buildx-$(cat /tmp/.version).$(echo $TARGETPLATFORM | sed 's/\//-/g')$(ls buildx* | sed -e 's/^buildx//')"

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
