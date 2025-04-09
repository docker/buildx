ARG NODE_VERSION=23.11
ARG ALPINE_VERSION=3.21
ARG GOLANG_VERSION=1.23
ARG JAEGERUI_VERSION=v1.68.0

FROM scratch AS jaegerui-src
ARG JAEGERUI_REPO=https://github.com/jaegertracing/jaeger-ui.git
ARG JAEGERUI_VERSION
ADD ${JAEGERUI_REPO}#${JAEGERUI_VERSION} /

FROM --platform=$BUILDPLATFORM node:${NODE_VERSION}-alpine${ALPINE_VERSION} AS builder
WORKDIR /work/jaeger-ui
COPY --from=jaegerui-src / .
RUN npm install
WORKDIR /work/jaeger-ui/packages/jaeger-ui
RUN NODE_ENVIRONMENT=production npm run build
# failed to find a way to avoid legacy build
RUN rm build/static/*-legacy* && rm build/static/*.png

FROM scratch AS jaegerui
COPY --from=builder /work/jaeger-ui/packages/jaeger-ui/build /

FROM alpine AS compressor
RUN --mount=target=/in,from=jaegerui <<EOT
    set -ex
    mkdir -p /out
    cp -a /in/. /out
    cd /out
    find . -type f -exec sh -c 'gzip -9 -c "$1" > "$1.tmp" && mv "$1.tmp" "$1"' _ {} \;
    # stop
EOT

FROM scratch AS jaegerui-compressed
COPY --from=compressor /out /
