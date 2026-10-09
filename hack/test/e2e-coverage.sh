#!/bin/bash

set -euo pipefail

COVERAGE_NAME="${COVERAGE_NAME:-e2e}"

OPERATOR_CONTROLLER_NAMESPACE="olmv1-system"
OPERATOR_CONTROLLER_MANAGER_DEPLOYMENT_NAME="operator-controller-controller-manager"

CATALOGD_NAMESPACE="olmv1-system"
CATALOGD_MANAGER_DEPLOYMENT_NAME="catalogd-controller-manager"

OBJECT_CONTROLLER_MANAGER_DEPLOYMENT_NAME="object-controller-controller-manager"

COPY_POD_NAME="e2e-coverage-copy-pod"

# Create a temporary directory for coverage
COVERAGE_OUTPUT=${ROOT_DIR}/coverage/${COVERAGE_NAME}.out
COVERAGE_DIR=${ROOT_DIR}/coverage/${COVERAGE_NAME}
rm -rf ${COVERAGE_DIR} && mkdir -p ${COVERAGE_DIR}

# Coverage-instrumented binary produces coverage on termination,
# so we scale down the manager before gathering the coverage
kubectl -n "$OPERATOR_CONTROLLER_NAMESPACE" scale deployment/"$OPERATOR_CONTROLLER_MANAGER_DEPLOYMENT_NAME" --replicas=0
kubectl -n "$CATALOGD_NAMESPACE" scale deployment/"$CATALOGD_MANAGER_DEPLOYMENT_NAME" --replicas=0

# Wait for manager pods to terminate so coverage data is flushed to the PVC
kubectl -n "$OPERATOR_CONTROLLER_NAMESPACE" wait --for=delete pods -l control-plane="$OPERATOR_CONTROLLER_MANAGER_DEPLOYMENT_NAME" --timeout=60s
kubectl -n "$CATALOGD_NAMESPACE" wait --for=delete pods -l control-plane="$CATALOGD_MANAGER_DEPLOYMENT_NAME" --timeout=60s

if object_controller_lookup=$(kubectl -n "$OPERATOR_CONTROLLER_NAMESPACE" get deployment/"$OBJECT_CONTROLLER_MANAGER_DEPLOYMENT_NAME" -o name 2>&1); then
    kubectl -n "$OPERATOR_CONTROLLER_NAMESPACE" scale deployment/"$OBJECT_CONTROLLER_MANAGER_DEPLOYMENT_NAME" --replicas=0
    kubectl -n "$OPERATOR_CONTROLLER_NAMESPACE" wait --for=delete pods -l control-plane="$OBJECT_CONTROLLER_MANAGER_DEPLOYMENT_NAME" --timeout=60s
else
    object_controller_lookup_status=$?
    if [[ "$object_controller_lookup" != *"Error from server (NotFound): deployments.apps \"$OBJECT_CONTROLLER_MANAGER_DEPLOYMENT_NAME\" not found"* ]]; then
        printf '%s\n' "$object_controller_lookup" >&2
        exit "$object_controller_lookup_status"
    fi
fi

# Copy the coverage data from the temporary pod
kubectl -n "$OPERATOR_CONTROLLER_NAMESPACE" cp "$COPY_POD_NAME":/e2e-coverage/ "$COVERAGE_DIR"

# Convert binary coverage data files into the textual format
go tool covdata textfmt -i "$COVERAGE_DIR" -o "$COVERAGE_OUTPUT"

echo "Coverage report generated successfully at: $COVERAGE_OUTPUT"
