#!/bin/bash

set -eux;

export BUILDER_DRIVER=docker-container

sh ./testings/builder/create_single_node_builder.sh

