#!/usr/bin/env bash

set -euo pipefail

# This script generates the registry+v1 bundle configuration JSON schema
# by extracting field information from v1alpha1.SubscriptionConfig and mapping
# those fields to their corresponding Kubernetes OpenAPI v3 schemas.

# Get the module path from go mod cache
if ! MODULE_PATH=$(go list -mod=readonly -m -f "{{.Dir}}" github.com/operator-framework/api); then
	echo "Error: Could not find github.com/operator-framework/api module" >&2
	exit 1
fi

# Source files
SUBSCRIPTION_TYPES="${MODULE_PATH}/pkg/operators/v1alpha1/subscription_types.go"

# Output file
SCHEMA_OUTPUT="internal/operator-controller/rukpak/bundle/registryv1bundleconfig.json"

# Verify required source file exists
if [[ ! -f "${SUBSCRIPTION_TYPES}" ]]; then
	echo "Error: ${SUBSCRIPTION_TYPES} not found." >&2
	echo "Module path: ${MODULE_PATH}" >&2
	exit 1
fi

# Get the effective k8s.io/api version (honors replace directives)
if ! K8S_API_VERSION=$(go list -m -f '{{.Version}}' k8s.io/api); then
	echo "Error: Could not determine k8s.io/api version" >&2
	exit 1
fi
if [[ -z "${K8S_API_VERSION}" ]]; then
	echo "Error: k8s.io/api version is empty" >&2
	exit 1
fi

# Convert k8s.io/api version (v0.35.0) to Kubernetes version (v1.35.0)
# k8s.io/api uses v0.X.Y while Kubernetes uses v1.X.Y
K8S_VERSION=$(echo "${K8S_API_VERSION}" | sed 's/^v0\./v1./' | tr -d '\n')

echo "$(date '+%Y/%m/%d %T') Detected k8s.io/api version: ${K8S_API_VERSION}"
echo "$(date '+%Y/%m/%d %T') Using Kubernetes version: ${K8S_VERSION}"

# Construct OpenAPI spec URL
OPENAPI_SPEC_URL="https://raw.githubusercontent.com/kubernetes/kubernetes/refs/tags/${K8S_VERSION}/api/openapi-spec/v3/api__v1_openapi.json"

echo "$(date '+%Y/%m/%d %T') Fetching Kubernetes OpenAPI spec from: ${OPENAPI_SPEC_URL}"
echo "$(date '+%Y/%m/%d %T') Generating registry+v1 bundle configuration JSON schema..."

# Run the schema generator
go run ./hack/tools/schema-generator "${OPENAPI_SPEC_URL}" "${SUBSCRIPTION_TYPES}" "${SCHEMA_OUTPUT}"

echo "$(date '+%Y/%m/%d %T') Schema generation complete: ${SCHEMA_OUTPUT}"
