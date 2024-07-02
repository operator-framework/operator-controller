#!/bin/bash

set -euo pipefail

TEST_CATALOG_NAME="test-catalog"
TEST_EXTENSION_NAME="test-package"

kubectl wait --for=condition=Available --timeout=60s -n olmv1-system deployment --all
kubectl wait --for=condition=Unpacked --timeout=60s ClusterCatalog $TEST_CATALOG_NAME
kubectl wait --for=condition=Installed --timeout=60s ClusterExtension $TEST_EXTENSION_NAME
