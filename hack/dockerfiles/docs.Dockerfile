# syntax = docker/dockerfile:1.2

FROM golang:1.16-alpine AS docsgen
WORKDIR /src
RUN --mount=target=. \
  --mount=target=/root/.cache,type=cache \
  go build -mod=vendor -o /out/docsgen ./docs/docsgen

FROM alpine AS gen
RUN apk add --no-cache rsync git
WORKDIR /src
COPY --from=docsgen /out/docsgen /usr/bin
RUN --mount=target=/context \
  --mount=target=.,type=tmpfs,readwrite  \
  rsync -a /context/. . && \
  docsgen && \
  mkdir /out && cp -r docs/reference /out

FROM scratch AS update
COPY --from=gen /out /out

FROM gen AS validate
RUN --mount=target=/context \
  --mount=target=.,type=tmpfs,readwrite  \
  rsync -a /context/. . && \
  git add -A && \
  rm -rf docs/reference/* && \
  cp -rf /out/* ./docs/ && \
  ./hack/validate-docs check
