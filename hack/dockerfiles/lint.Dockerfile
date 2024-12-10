# syntax=docker/dockerfile:1

ARG GO_VERSION=1.23
ARG ALPINE_VERSION=3.21
ARG XX_VERSION=1.6.1

ARG GOLANGCI_LINT_VERSION=1.62.0
ARG GOPLS_VERSION=v0.26.0
# disabled: deprecated unusedvariable simplifyrange
ARG GOPLS_ANALYZERS="embeddirective fillreturns infertypeargs nonewvars norangeoverfunc noresultvalues simplifycompositelit simplifyslice undeclaredname unusedparams useany"

FROM --platform=$BUILDPLATFORM tonistiigi/xx:${XX_VERSION} AS xx

FROM --platform=$BUILDPLATFORM golang:${GO_VERSION}-alpine${ALPINE_VERSION} AS golang-base
RUN apk add --no-cache git gcc musl-dev

FROM golang-base AS lint-base
ENV GOFLAGS="-buildvcs=false"
ARG GOLANGCI_LINT_VERSION
RUN wget -O- -nv https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s v${GOLANGCI_LINT_VERSION}
COPY --link --from=xx / /
WORKDIR /go/src/github.com/docker/buildx
ARG TARGETPLATFORM

FROM lint-base AS lint
RUN --mount=target=/go/src/github.com/docker/buildx \
    --mount=target=/root/.cache,type=cache,id=lint-cache-$TARGETPLATFORM \
    xx-go --wrap && \
    golangci-lint run

FROM lint-base AS validate-golangci
RUN --mount=target=/go/src/github.com/docker/buildx \
  golangci-lint config verify

FROM golang-base AS gopls
RUN apk add --no-cache git
ARG GOPLS_VERSION
WORKDIR /src
RUN git clone https://github.com/golang/tools.git && \
  cd tools && git checkout ${GOPLS_VERSION}
WORKDIR tools/gopls
ARG GOPLS_ANALYZERS
RUN <<'EOF'
  set -ex
  mkdir -p /out
  for analyzer in ${GOPLS_ANALYZERS}; do
    mkdir -p internal/cmd/$analyzer
    cat <<eot > internal/cmd/$analyzer/main.go
package main

import (
	"golang.org/x/tools/go/analysis/singlechecker"
	analyzer "golang.org/x/tools/gopls/internal/analysis/$analyzer"
)

func main() { singlechecker.Main(analyzer.Analyzer) }
eot
    echo "Analyzing with ${analyzer}..."
    go build -o /out/$analyzer ./internal/cmd/$analyzer
  done
EOF

FROM golang-base AS gopls-analyze
COPY --link --from=xx / /
ARG GOPLS_ANALYZERS
ARG TARGETNAME
ARG TARGETPLATFORM
WORKDIR /go/src/github.com/docker/buildx
RUN --mount=target=. \
  --mount=target=/root/.cache,type=cache,id=lint-cache-${TARGETNAME}-${TARGETPLATFORM} \
  --mount=target=/gopls-analyzers,from=gopls,source=/out <<EOF
  set -ex
  xx-go --wrap
  for analyzer in ${GOPLS_ANALYZERS}; do
    go vet -vettool=/gopls-analyzers/$analyzer ./...
  done
EOF

FROM lint
