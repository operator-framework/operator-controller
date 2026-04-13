#!/bin/bash

set -euo pipefail

COVERAGE_NAME="${COVERAGE_NAME:-e2e}"

OPERATOR_CONTROLLER_NAMESPACE="olmv1-system"
OPERATOR_CONTROLLER_MANAGER_DEPLOYMENT_NAME="operator-controller-controller-manager"

CATALOGD_NAMESPACE="olmv1-system"
CATALOGD_MANAGER_DEPLOYMENT_NAME="catalogd-controller-manager"

COPY_POD_NAME="e2e-coverage-copy-pod"

# Create a temporary directory for coverage
COVERAGE_OUTPUT=${ROOT_DIR}/coverage/${COVERAGE_NAME}.out
COVERAGE_DIR=${ROOT_DIR}/coverage/${COVERAGE_NAME}
rm -rf ${COVERAGE_DIR} && mkdir -p ${COVERAGE_DIR}

# Coverage-instrumented binary produces coverage on termination,
# so we scale down the manager before gathering the coverage
kubectl -n "$OPERATOR_CONTROLLER_NAMESPACE" scale deployment/"$OPERATOR_CONTROLLER_MANAGER_DEPLOYMENT_NAME" --replicas=0
kubectl -n "$CATALOGD_NAMESPACE" scale deployment/"$CATALOGD_MANAGER_DEPLOYMENT_NAME" --replicas=0

# Wait for deployments to scale down so coverage data is flushed to the PVC
kubectl -n "$OPERATOR_CONTROLLER_NAMESPACE" wait --for=jsonpath='{.status.replicas}'=0 deployment/"$OPERATOR_CONTROLLER_MANAGER_DEPLOYMENT_NAME" --timeout=60s
kubectl -n "$CATALOGD_NAMESPACE" wait --for=jsonpath='{.status.replicas}'=0 deployment/"$CATALOGD_MANAGER_DEPLOYMENT_NAME" --timeout=60s

# Copy the coverage data from the temporary pod
kubectl -n "$OPERATOR_CONTROLLER_NAMESPACE" cp "$COPY_POD_NAME":/e2e-coverage/ "$COVERAGE_DIR"

# Convert binary coverage data files into the textual format
go tool covdata textfmt -i "$COVERAGE_DIR" -o "$COVERAGE_OUTPUT"

echo "Coverage report generated successfully at: $COVERAGE_OUTPUT"
