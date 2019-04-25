# syntax=docker/dockerfile:1.0-experimental

ARG DOCKERD_VERSION=19.03-rc
ARG CLI_VERSION=19.03

FROM docker:$DOCKERD_VERSION AS dockerd-release

# xgo is a helper for golang cross-compilation
FROM --platform=$BUILDPLATFORM tonistiigi/xx:golang@sha256:6f7d999551dd471b58f70716754290495690efa8421e0a1fcf18eb11d0c0a537 AS xgo

FROM --platform=$BUILDPLATFORM golang:1.12-alpine AS gobase
COPY --from=xgo / /
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
ARG TARGETPLATFORM
RUN --mount=target=. --mount=target=/root/.cache,type=cache \
  --mount=target=/go/pkg/mod,type=cache \
  --mount=source=/tmp/.ldflags,target=/tmp/.ldflags,from=buildx-version \
  set -x; go build -ldflags "$(cat /tmp/.ldflags)" -o /usr/bin/buildx ./cmd/buildx && \
  file /usr/bin/buildx && file /usr/bin/buildx | egrep "statically linked|Mach-O|Windows"

FROM buildx-build AS integration-tests
COPY . .

FROM golang:1.12-alpine AS docker-cli-build
RUN apk add -U git bash coreutils gcc musl-dev
ENV CGO_ENABLED=0
ARG REPO=github.com/tiborvass/cli
ARG BRANCH=cli-plugin-aliases
ARG CLI_VERSION
WORKDIR /go/src/github.com/docker/cli
RUN git clone git://$REPO . && git checkout $BRANCH
RUN ./scripts/build/binary

FROM scratch AS binaries-unix
COPY --from=buildx-build /usr/bin/buildx /

FROM binaries-unix AS binaries-darwin
FROM binaries-unix AS binaries-linux

FROM scratch AS binaries-windows
COPY --from=buildx-build /usr/bin/buildx /buildx.exe

FROM binaries-$TARGETOS AS binaries

FROM --platform=$BUILDPLATFORM alpine AS releaser
WORKDIR /work
ARG TARGETPLATFORM
RUN --mount=from=binaries \
  --mount=source=/tmp/.version,target=/tmp/.version,from=buildx-version \
  mkdir -p /out && cp buildx* "/out/buildx-$(cat /tmp/.version).$(echo $TARGETPLATFORM | sed 's/\//-/g')$(ls buildx* | sed -e 's/^buildx//')"

FROM scratch AS release
COPY --from=releaser /out/ /

FROM alpine AS demo-env
RUN apk add --no-cache iptables tmux git vim less openssh
RUN mkdir -p /usr/local/lib/docker/cli-plugins && ln -s /usr/local/bin/buildx /usr/local/lib/docker/cli-plugins/docker-buildx
COPY ./hack/demo-env/entrypoint.sh /usr/local/bin
COPY ./hack/demo-env/tmux.conf /root/.tmux.conf
COPY --from=dockerd-release /usr/local/bin /usr/local/bin
COPY --from=docker-cli-build /go/src/github.com/docker/cli/build/docker /usr/local/bin

WORKDIR /work
COPY ./hack/demo-env/examples .
COPY --from=binaries / /usr/local/bin/
VOLUME /var/lib/docker
ENTRYPOINT ["entrypoint.sh"]

FROM binaries