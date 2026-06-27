# syntax=docker/dockerfile:1

ARG GO_VERSION=1.26
ARG ALPINE_VERSION=3.23

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
ARG BUILDX_EXPERIMENTAL
RUN --mount=target=/context \
  --mount=target=.,type=tmpfs <<EOT
set -e
rsync -a /context/. .
docsgen --formats "$FORMATS" --source "docs/reference/" --bake-stdlib-source "docs/bake-stdlib.md"
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

# Replace the checked-in docs with freshly generated output, then verify that
# the working tree still matches HEAD under these paths. This enforces the
# same contract as `make docs`: after regenerating, the repo must be clean.
rsync -a --delete /out/reference/ docs/reference/
cp /out/bake-stdlib.md docs/bake-stdlib.md

# Intent-to-add so untracked files (e.g. a new command's docs) appear in the
# diff below; `git diff` skips untracked files by default.
git add -N -- docs/reference docs/bake-stdlib.md

if [ -n "$(git status --porcelain -- docs/reference docs/bake-stdlib.md)" ]; then
  echo >&2 'ERROR: Docs are out of date. Run "make docs" and commit the result.'
  echo >&2
  echo >&2 '--- changed paths ---'
  git status --porcelain -- docs/reference docs/bake-stdlib.md
  echo >&2
  echo >&2 '--- diff (truncated to 200 lines) ---'
  git diff -- docs/reference docs/bake-stdlib.md | head -n 200
  exit 1
fi
EOT
