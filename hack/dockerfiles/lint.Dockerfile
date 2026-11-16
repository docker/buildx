# syntax=docker/dockerfile:1

ARG GO_VERSION=1.27
ARG ALPINE_VERSION=3.24
ARG XX_VERSION=1.10.0

ARG GOLANGCI_LINT_VERSION=v2.14.0
ARG GOLANGCI_FROM_SOURCE=true
ARG GOPLS_ANALYZERS_VERSION=v0.3.0

FROM --platform=$BUILDPLATFORM tonistiigi/xx:${XX_VERSION} AS xx

FROM --platform=$BUILDPLATFORM golang:${GO_VERSION}-alpine${ALPINE_VERSION} AS base
RUN apk add --no-cache git gcc musl-dev binutils-gold

FROM base AS golangci-build
WORKDIR /src
ARG GOLANGCI_LINT_VERSION
ADD --keep-git-dir=true https://github.com/golangci/golangci-lint.git#${GOLANGCI_LINT_VERSION} .
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

FROM base AS gopls-analyzers
ARG GOPLS_ANALYZERS_VERSION
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/ \
  GOBIN=/out go install github.com/crazy-max/gopls-analyzers/cmd/gopls-analyzers@${GOPLS_ANALYZERS_VERSION}

FROM base AS gopls-analyze
COPY --link --from=xx / /
ARG TARGETNAME
ARG TARGETPLATFORM
WORKDIR /go/src/github.com/docker/buildx
RUN --mount=target=. \
  --mount=target=/root/.cache,type=cache,id=lint-cache-${TARGETNAME}-${TARGETPLATFORM} \
  --mount=target=/usr/bin/gopls-analyzers,from=gopls-analyzers,source=/out/gopls-analyzers <<EOF
  set -ex
  xx-go --wrap
  gopls-analyzers -list-analyzers
  go vet -vettool=/usr/bin/gopls-analyzers -exclude-generated ./...
EOF

FROM base AS gopls-fix-run
COPY --link --from=xx / /
ARG TARGETNAME
ARG TARGETPLATFORM
WORKDIR /go/src/github.com/docker/buildx
RUN --mount=target=.,rw \
  --mount=target=/root/.cache,type=cache,id=lint-cache-${TARGETNAME}-${TARGETPLATFORM} \
  --mount=target=/usr/bin/gopls-analyzers,from=gopls-analyzers,source=/out/gopls-analyzers <<EOF
  set -ex
  xx-go --wrap
  mkdir /out
  /usr/bin/gopls-analyzers -fix -exclude-generated ./...
  for file in $(git status --porcelain | awk '/^ M/ {print $2}'); do
    mkdir -p /out/$(dirname $file)
    cp $file /out/$file
  done
EOF

FROM scratch AS gopls-fix
COPY --link --from=gopls-fix-run /out /

FROM lint
