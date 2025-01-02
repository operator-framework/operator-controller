#!/bin/bash

set -euo pipefail


help="pre-upgrade-setup.sh is used to create some basic resources
which will later be used in upgrade testing.

Usage:
  pre-upgrade-setup.sh [TEST_CLUSTER_CATALOG_IMAGE] [TEST_CLUSTER_CATALOG_NAME]
"

if [[ "$#" -ne 2 ]]; then
  echo "Illegal number of arguments passed"
  echo "${help}"
  exit 1
fi

export TEST_CLUSTER_CATALOG_IMAGE=$1
export TEST_CLUSTER_CATALOG_NAME=$2

kubectl apply -f - << EOF
apiVersion: olm.operatorframework.io/v1
kind: ClusterCatalog
metadata:
  name: ${TEST_CLUSTER_CATALOG_NAME}
spec:
  source:
    type: Image
    image:
      ref: ${TEST_CLUSTER_CATALOG_IMAGE}
EOF

kubectl wait --for=condition=Serving --timeout=60s ClusterCatalog "$TEST_CLUSTER_CATALOG_NAME"
