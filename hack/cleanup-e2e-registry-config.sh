#!/bin/bash
set -o errexit
set -o nounset
set -o pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
CERTS_DIR="${PROJECT_ROOT}/hack/kind-config/containerd/certs.d"

REGISTRY_HOST="docker-registry.operator-controller-e2e.svc.cluster.local:5000"
REGISTRY_DIR="${CERTS_DIR}/${REGISTRY_HOST}"

echo "Cleaning up e2e registry configuration..."

if [ -d "${REGISTRY_DIR}" ]; then
    echo "Removing directory: ${REGISTRY_DIR}"
    rm -rf "${REGISTRY_DIR}"
    echo "E2E registry configuration cleanup complete."
else
    echo "Registry directory not found, nothing to clean."
fi
