#!/bin/bash
set -o errexit
set -o nounset
set -o pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
CERTS_DIR="${PROJECT_ROOT}/hack/kind-config/containerd/certs.d"

REGISTRY_HOST="docker-registry.operator-controller-e2e.svc.cluster.local:5000"
REGISTRY_DIR="${CERTS_DIR}/${REGISTRY_HOST}"

echo "Setting up e2e registry configuration..."
echo "Creating directory: ${REGISTRY_DIR}"

mkdir -p "${REGISTRY_DIR}"

cat > "${REGISTRY_DIR}/hosts.toml" << 'EOF'
[host."https://localhost:30000"]
  capabilities = ["pull", "resolve"]
  skip_verify = true
EOF

echo "Created ${REGISTRY_DIR}/hosts.toml"
echo "E2E registry configuration setup complete."
