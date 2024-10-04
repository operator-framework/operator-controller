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
apiVersion: olm.operatorframework.io/v1alpha1
kind: ClusterCatalog
metadata:
  name: ${TEST_CLUSTER_CATALOG_NAME}
spec:
  source:
    type: Image
    image:
      ref: ${TEST_CATALOG_IMG}
      pollInterval: 24h
EOF

kubectl apply -f - <<EOF
apiVersion: v1
kind: ServiceAccount
metadata:
  name: upgrade-e2e
  namespace: default
EOF

kubectl apply -f - <<EOF
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: upgrade-e2e
rules:
  - apiGroups:
    - ""
    resources:
    - "secrets"
    - "services"
    - "serviceaccounts"
    verbs:
    - "create"
    - "update"
    - "patch"
    - "delete"
    - "get"
    - "list"
    - "watch"
  - apiGroups:
    - "apps"
    resources:
    - "deployments"
    verbs:
    - "create"
    - "update"
    - "patch"
    - "delete"
    - "get"
    - "list"
    - "watch"
  - apiGroups:
    - "apiextensions.k8s.io"
    resources:
    - "customresourcedefinitions"
    verbs:
    - "create"
    - "update"
    - "patch"
    - "delete"
    - "get"
    - "list"
    - "watch"
  - apiGroups:
    - "rbac.authorization.k8s.io"
    resources:
    - "clusterroles"
    - "clusterrolebindings"
    - "roles"
    - "rolebindings"
    verbs:
    - "create"
    - "update"
    - "patch"
    - "delete"
    - "get"
    - "list"
    - "watch"
    - "bind"
    - "escalate"
  - apiGroups:
    - "olm.operatorframework.io"
    resources:
    - "clusterextensions/finalizers"
    verbs:
    - "update"
    resourceNames:
    - "${TEST_CLUSTER_EXTENSION_NAME}"
EOF

kubectl apply -f - <<EOF
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: upgrade-e2e
subjects:
  - kind: ServiceAccount
    name: upgrade-e2e
    namespace: default
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: upgrade-e2e
EOF

kubectl apply -f - << EOF
apiVersion: olm.operatorframework.io/v1alpha1
kind: ClusterExtension
metadata:
  name: ${TEST_CLUSTER_EXTENSION_NAME}
spec:
  source:
    sourceType: Catalog
    catalog:
      packageName: prometheus
      version: 1.0.0
  install:
    namespace: default
    serviceAccount:
      name: upgrade-e2e
EOF

kubectl wait --for=condition=Serving --timeout=60s ClusterCatalog $TEST_CLUSTER_CATALOG_NAME
kubectl wait --for=condition=Installed --timeout=60s ClusterExtension $TEST_CLUSTER_EXTENSION_NAME
