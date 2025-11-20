#!/bin/bash


set -euo pipefail

# This script is used to install OLMv1 from the main branch by:
# 1. Checking out main branch in a temporary location
# 2. Building container images from main source code
# 3. Loading images into the kind cluster
# 4. Deploying manifests from main


help="install-from-main.sh installs OLMv1 from main branch source code

Usage:
  install-from-main.sh [MANIFEST_NAME]

Example:
  install-from-main.sh experimental.yaml
  install-from-main.sh standard.yaml

Environment variables:
  KIND_CLUSTER_NAME: Name of the kind cluster (required)
"

if [[ "$#" -ne 1 ]]; then
  echo "Illegal number of arguments passed"
  echo "${help}"
  exit 1
fi

if [[ -z "${KIND_CLUSTER_NAME:-}" ]]; then
  echo "Error: KIND_CLUSTER_NAME environment variable must be set"
  exit 1
fi

MANIFEST_NAME=$1

# Create temporary directory for main branch checkout
TEMP_DIR=$(mktemp -d)
trap 'echo "Cleaning up ${TEMP_DIR}"; rm -rf "${TEMP_DIR}"' EXIT

echo "Cloning main branch to temporary directory..."
# Clone from GitHub (works in CI and locally)
git clone --depth 1 --branch main https://github.com/operator-framework/operator-controller.git "${TEMP_DIR}"

cd "${TEMP_DIR}"

echo "Building images from main branch source code..."
make docker-build

echo "Loading images into kind cluster ${KIND_CLUSTER_NAME}..."
make kind-load KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME}"

echo "Deploying manifests from main branch..."
# Extract CERT_MGR_VERSION from main branch Makefile
# Expected Makefile line format: export CERT_MGR_VERSION := <value>
CERT_MGR_VERSION_FROM_MAIN=$(grep "^export CERT_MGR_VERSION" Makefile | awk -F'[ :=]+' '{print $NF}')
export CERT_MGR_VERSION="${CERT_MGR_VERSION_FROM_MAIN:-v1.18.2}"
export MANIFEST="https://raw.githubusercontent.com/operator-framework/operator-controller/main/manifests/${MANIFEST_NAME}"
export DEFAULT_CATALOG="https://raw.githubusercontent.com/operator-framework/operator-controller/main/manifests/default-catalogs.yaml"
export INSTALL_DEFAULT_CATALOGS="${INSTALL_DEFAULT_CATALOGS:-false}"

curl -L -s https://raw.githubusercontent.com/operator-framework/operator-controller/main/scripts/install.tpl.sh | \
  envsubst '$$DEFAULT_CATALOG,$$CERT_MGR_VERSION,$$INSTALL_DEFAULT_CATALOGS,$$MANIFEST' | bash -s

echo "Successfully installed OLMv1 from main branch"

