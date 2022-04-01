# syntax=docker/dockerfile:1.4

ARG GO_VERSION=1.17

FROM golang:${GO_VERSION}-alpine
RUN apk add --no-cache gcc musl-dev yamllint
RUN wget -O- -nv https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s v1.36.0
WORKDIR /go/src/github.com/docker/buildx
RUN --mount=target=/go/src/github.com/docker/buildx --mount=target=/root/.cache,type=cache \
  golangci-lint run
RUN --mount=target=/go/src/github.com/docker/buildx --mount=target=/root/.cache,type=cache \
  yamllint -c .yamllint.yml --strict .
