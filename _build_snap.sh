#!/usr/bin/env bash

set -euo pipefail

DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )/../"

make clean

echo "building snap"
echo ""
cd "$DIR" && docker run --rm -v "$DIR":/build -w /build sammytheshark/doctl-snap-base
