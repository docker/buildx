#!/bin/bash

set -eux;

export BUILDER_DRIVER=kubernetes
export KUBECONFIG=${KUBECONFIG:-~/.kube/config}

sh ./testings/builder/create_single_node_builder.sh

