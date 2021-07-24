#!/bin/bash

BUILDER_NAME=${BUILDER_NAME:-builder}
BUILDER_DRIVER=${BUILDER_DRIVER:-docker-container}

docker buildx create \
  --name="${BUILDER_NAME}" \
  --driver="${BUILDER_DRIVER}" \
  --platform=linux/amd64,linux/arm64

docker buildx inspect "${BUILDER_NAME}" --bootstrap
docker buildx use "${BUILDER_NAME}"