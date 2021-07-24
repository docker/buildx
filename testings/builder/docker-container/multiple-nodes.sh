#!/bin/bash

set -eux;

export BUILDER_DRIVER=docker-container

sh ./testings/builder/create_multiple_nodes_builder.sh

