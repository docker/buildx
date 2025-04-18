#!/bin/bash
# Copyright 2022 The Go Authors. All rights reserved.
# Use of this source code is governed by a BSD-style
# license that can be found in the LICENSE file.

# Runs the integration tests for whole program analysis.
# Assumes this is run from vuln/cmd/govulncheck/integration

echo "Building govulncheck docker image"
# The building context is vuln/ so we can have the current
# version of both govulncheck and its vuln dependencies
docker build -f Dockerfile -t govulncheck-integration ../../../

echo "Running govulncheck integration tests in the docker image"
docker run govulncheck-integration ./integration_run.sh
