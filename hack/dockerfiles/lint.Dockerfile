# syntax=docker/dockerfile:1

ARG GO_VERSION=1.21.6
ARG GOLANGCI_LINT_VERSION=1.54.2

FROM golang:${GO_VERSION}-alpine
RUN apk add --no-cache git gcc musl-dev
ENV GOFLAGS="-buildvcs=false"
ARG GOLANGCI_LINT_VERSION
RUN wget -O- -nv https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s v${GOLANGCI_LINT_VERSION}
WORKDIR /go/src/github.com/docker/buildx
RUN --mount=target=/go/src/github.com/docker/buildx \
    --mount=target=/root/.cache,type=cache \
    golangci-lint run
