#!/bin/bash

set -euo pipefail

COVERAGE_OUTPUT="${COVERAGE_OUTPUT:-e2e-cover.out}"

OPERATOR_CONTROLLER_NAMESPACE="operator-controller-system"
OPERATOR_CONTROLLER_MANAGER_DEPLOYMENT_NAME="operator-controller-controller-manager"
COPY_POD_NAME="e2e-coverage-copy-pod"

# Create a temporary directory for coverage
COVERAGE_DIR=$(mktemp -d)

# Trap to cleanup temporary directory on script exit
function cleanup {
    rm -rf "$COVERAGE_DIR"
}
trap cleanup EXIT

# Coverage-instrumented binary produces coverage on termination,
# so we scale down the manager before gathering the coverage
kubectl -n "$OPERATOR_CONTROLLER_NAMESPACE" scale deployment/"$OPERATOR_CONTROLLER_MANAGER_DEPLOYMENT_NAME" --replicas=0

# Wait for the copy pod to be ready
kubectl -n "$OPERATOR_CONTROLLER_NAMESPACE" wait --for=condition=ready pod "$COPY_POD_NAME"

# Copy the coverage data from the temporary pod
kubectl -n "$OPERATOR_CONTROLLER_NAMESPACE" cp "$COPY_POD_NAME":/e2e-coverage/ "$COVERAGE_DIR"

# Convert binary coverage data files into the textual format
go tool covdata textfmt -i "$COVERAGE_DIR" -o "$COVERAGE_OUTPUT"

echo "Coverage report generated successfully at: $COVERAGE_OUTPUT"
