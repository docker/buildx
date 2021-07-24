#!/bin/bash

set -eux;

export KUBECONFIG=${KUBECONFIG:-~/.kube/config}
export BUILDER_DRIVER=kubernetes

sh ./testings/builder/create_multiple_nodes_builder.sh

