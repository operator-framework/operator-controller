#!/bin/bash

set -euo pipefail

help="pre-upgrade-setup.sh is used to create some basic resources
which will later be used in upgrade testing.

Usage:
  post-upgrade-checks.sh [TEST_CATALOG_IMG] [TEST_CATALOG_NAME] [TEST_CLUSTER_EXTENSION_NAME]
"

if [[ "$#" -ne 3 ]]; then
  echo "Illegal number of arguments passed"
  echo "${help}"
  exit 1
fi

TEST_CATALOG_IMG=$1
TEST_CLUSTER_CATALOG_NAME=$2
TEST_CLUSTER_EXTENSION_NAME=$3

kubectl apply -f - << EOF
apiVersion: catalogd.operatorframework.io/v1alpha1
kind: ClusterCatalog
metadata:
  name: ${TEST_CLUSTER_CATALOG_NAME}
spec:
  source:
    type: image
    image:
      ref: ${TEST_CATALOG_IMG}
      pollInterval: 24h
      insecureSkipTLSVerify: true
EOF


kubectl apply -f - << EOF
apiVersion: olm.operatorframework.io/v1alpha1
kind: ClusterExtension
metadata:
  name: ${TEST_CLUSTER_EXTENSION_NAME}
spec:
  installNamespace: default
  packageName: prometheus
  version: 1.0.0
  serviceAccount:
    name: default
EOF

kubectl wait --for=condition=Unpacked --timeout=60s ClusterCatalog $TEST_CLUSTER_CATALOG_NAME
kubectl wait --for=condition=Installed --timeout=60s ClusterExtension $TEST_CLUSTER_EXTENSION_NAME
