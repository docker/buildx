#!/usr/bin/env bash

set -ex

rm -rf ./out

docker buildx bake --print
docker buildx bake --sync-output

if [[ ! -f ./out/foo || ! -f ./out/bar ]]; then
  echo >&2 "error: missing output files"
  exit 1
fi
