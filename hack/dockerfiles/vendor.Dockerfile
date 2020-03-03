# syntax = docker/dockerfile:1.0-experimental
FROM golang:1.13-alpine AS vendored
RUN  apk add --no-cache git rsync
WORKDIR /src
RUN --mount=target=/context \
  --mount=target=.,type=tmpfs,readwrite  \
  --mount=target=/go/pkg/mod,type=cache \
  rsync -a /context/. . && \
  go mod tidy && go mod vendor && \
  mkdir /out && cp -r go.mod go.sum vendor /out

FROM scratch AS update
COPY --from=vendored /out /out

FROM vendored AS validate
RUN --mount=target=/context \
  --mount=target=.,type=tmpfs,readwrite  \
  rsync -a /context/. . && \
  git add -A && \
  rm -rf vendor && \
  cp -rf /out/* . && \
  ./hack/validate-vendor check
