# syntax=docker/dockerfile:1

ARG GO_VERSION=1.21
ARG XX_VERSION=1.3.0
ARG GOLANGCI_LINT_VERSION=1.54.2

FROM --platform=$BUILDPLATFORM tonistiigi/xx:${XX_VERSION} AS xx
FROM --platform=$BUILDPLATFORM golang:${GO_VERSION}-alpine
RUN apk add --no-cache git gcc musl-dev
ENV GOFLAGS="-buildvcs=false"
ARG GOLANGCI_LINT_VERSION
RUN wget -O- -nv https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s v${GOLANGCI_LINT_VERSION}
COPY --link --from=xx / /
WORKDIR /go/src/github.com/docker/buildx
ARG TARGETPLATFORM
RUN --mount=target=/go/src/github.com/docker/buildx \
    --mount=target=/root/.cache,type=cache,id=lint-cache-$TARGETPLATFORM \
    xx-go --wrap && \
    golangci-lint run
