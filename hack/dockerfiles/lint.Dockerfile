# syntax=docker/dockerfile:1

ARG GO_VERSION=1.25
ARG ALPINE_VERSION=3.22
ARG XX_VERSION=1.8.0

ARG GOLANGCI_LINT_VERSION=v2.1.5
ARG GOLANGCI_FROM_SOURCE=false
ARG GOPLS_VERSION=v0.33.0
# GOPLS_ANALYZERS defines gopls analyzers to be run. disabled by default: deprecated simplifyrange unusedfunc unusedvariable
ARG GOPLS_ANALYZERS="embeddirective fillreturns infertypeargs maprange modernize nonewvars noresultvalues simplifycompositelit simplifyslice unusedparams yield"

FROM --platform=$BUILDPLATFORM tonistiigi/xx:${XX_VERSION} AS xx

FROM --platform=$BUILDPLATFORM golang:${GO_VERSION}-alpine${ALPINE_VERSION} AS base
RUN apk add --no-cache git gcc musl-dev binutils-gold

FROM base AS golangci-build
WORKDIR /src
ARG GOLANGCI_LINT_VERSION
ADD https://github.com/golangci/golangci-lint.git#${GOLANGCI_LINT_VERSION} .
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/ go mod download
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/ mkdir -p out && go build -o /out/golangci-lint ./cmd/golangci-lint

FROM scratch AS golangci-binary-false
FROM scratch AS golangci-binary-true
COPY --from=golangci-build /out/golangci-lint golangci-lint
FROM golangci-binary-${GOLANGCI_FROM_SOURCE} AS golangci-binary

FROM base AS lint-base
ENV GOFLAGS="-buildvcs=false"
ARG GOLANGCI_LINT_VERSION
ARG GOLANGCI_FROM_SOURCE
COPY --link --from=golangci-binary / /usr/bin/
RUN [ "${GOLANGCI_FROM_SOURCE}" = "true" ] && exit 0; wget -O- -nv https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s ${GOLANGCI_LINT_VERSION}
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

FROM base AS gopls
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

FROM base AS gopls-analyze
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

FROM base AS modernize-fix-run
COPY --link --from=xx / /
ARG TARGETNAME
ARG TARGETPLATFORM
WORKDIR /go/src/github.com/docker/buildx
RUN --mount=target=.,rw \
  --mount=target=/root/.cache,type=cache,id=lint-cache-${TARGETNAME}-${TARGETPLATFORM} \
  --mount=target=/gopls-analyzers,from=gopls,source=/out <<EOF
  set -ex
  xx-go --wrap
  mkdir /out
  /gopls-analyzers/modernize -fix ./...
  for file in $(git status --porcelain | awk '/^ M/ {print $2}'); do
    mkdir -p /out/$(dirname $file)
    cp $file /out/$file
  done
EOF

FROM scratch AS modernize-fix
COPY --link --from=modernize-fix-run /out /

FROM lint
