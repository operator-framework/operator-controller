#!/bin/bash

set -euo pipefail

help="pre-upgrade-setup.sh is used to create some basic resources
which will later be used in upgrade testing.

The following environment variables are required for configuring this script:
- \$CATALOG_IMG - the tag for the catalog image that contains the registry+v1 bundle.
"

TEST_CATALOG_NAME="test-catalog"
TEST_EXTENSION_NAME="test-package"

if [[ -z "${CATALOG_IMG}" ]]; then
  echo "\$CATALOG_IMG is required to be set"
  echo "${help}"
  exit 1
fi

kubectl apply -f - << EOF
apiVersion: catalogd.operatorframework.io/v1alpha1
kind: ClusterCatalog
metadata:
  name: ${TEST_CATALOG_NAME}
spec:
  source:
    type: image
    image:
      ref: ${CATALOG_IMG}
      pollInterval: 24h
      insecureSkipTLSVerify: true
EOF


kubectl apply -f - << EOF
apiVersion: olm.operatorframework.io/v1alpha1
kind: ClusterExtension
metadata:
  name: ${TEST_EXTENSION_NAME}
spec:
  installNamespace: default
  packageName: prometheus
EOF

kubectl wait --for=condition=Unpacked --timeout=60s ClusterCatalog $TEST_CATALOG_NAME
kubectl wait --for=condition=Installed --timeout=60s ClusterExtension $TEST_EXTENSION_NAME
