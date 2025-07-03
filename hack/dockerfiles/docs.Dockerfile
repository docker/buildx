# syntax=docker/dockerfile:1

ARG GO_VERSION=1.24
ARG ALPINE_VERSION=3.22

ARG FORMATS=md,yaml

FROM golang:${GO_VERSION}-alpine${ALPINE_VERSION} AS docsgen
WORKDIR /src
RUN --mount=target=. \
  --mount=target=/root/.cache,type=cache \
  go build -mod=vendor -o /out/docsgen ./docs/generate.go

FROM alpine:${ALPINE_VERSION} AS gen
RUN apk add --no-cache rsync git
WORKDIR /src
COPY --from=docsgen /out/docsgen /usr/bin
ARG FORMATS
RUN --mount=target=/context \
  --mount=target=.,type=tmpfs <<EOT
set -e
rsync -a /context/. .
docsgen --formats "$FORMATS" --source "docs/reference" --bake-stdlib-source "docs/bake-stdlib.md"
mkdir /out
cp -r docs/reference docs/bake-stdlib.md /out
rm -f /out/reference/*__INTERNAL_SERVE.yaml /out/reference/*__INTERNAL_SERVE.md
EOT

FROM scratch AS update
COPY --from=gen /out /out

FROM gen AS validate
RUN --mount=target=/context \
  --mount=target=.,type=tmpfs <<EOT
set -e
rsync -a /context/. .
git add -A
rm -rf docs/reference/* docs/bake-stdlib.md
cp -rf /out/* ./docs/
if [ -n "$(git status --porcelain -- docs/reference docs/bake-stdlib.md)" ]; then
  echo >&2 'ERROR: Docs result differs. Please update with "make docs"'
  git status --porcelain -- docs/reference docs/bake-stdlib.md
  exit 1
fi
EOT
