# syntax=docker/dockerfile:1

ARG GO_VERSION=1.20.8

FROM golang:${GO_VERSION}-alpine
RUN apk add --no-cache git gcc musl-dev
RUN wget -O- -nv https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s v1.51.1
WORKDIR /go/src/github.com/docker/buildx
RUN --mount=target=/go/src/github.com/docker/buildx --mount=target=/root/.cache,type=cache \
  golangci-lint run
