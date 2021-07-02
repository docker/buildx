# syntax = docker/dockerfile:1.2

FROM golang:1.16-alpine AS yamlgen
WORKDIR /src
RUN --mount=target=. \
  --mount=target=/root/.cache,type=cache \
  go build -mod=vendor -o /out/yamlgen ./docs/yamlgen

FROM alpine AS gen
RUN apk add --no-cache rsync git
WORKDIR /src
COPY --from=yamlgen /out/yamlgen /usr/bin
RUN --mount=target=/context \
  --mount=target=.,type=tmpfs,readwrite  \
  rsync -a /context/. . \
  && yamlgen --target /out/yaml

FROM scratch AS update
COPY --from=gen /out /
