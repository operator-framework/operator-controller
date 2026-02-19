#!/usr/bin/env bash
set -euo pipefail

# This script builds and deploys operator-controller from the main branch
# as the baseline for experimental-to-experimental upgrade tests.
# Instead of upgrading from the latest release, this allows comparing
# main -> PR for experimental features that may not exist in any release yet.
#
# Required environment variables (exported by Makefile):
#   KIND_CLUSTER_NAME  - name of the kind cluster
#   OPCON_IMAGE_REPO   - operator-controller image repository
#   CATD_IMAGE_REPO    - catalogd image repository

MAIN_TAG=main

# Save current HEAD so we can return after building from main
CURRENT_REF=$(git rev-parse HEAD)
cleanup() {
    echo "Returning to ${CURRENT_REF}"
    git checkout -f "${CURRENT_REF}"
}
trap cleanup EXIT

# Fetch and checkout main
echo "Fetching and checking out origin/main"
git fetch origin main
git checkout FETCH_HEAD

# Build images from main with a distinct tag so the upgrade
# (which uses the default 'devel' tag) triggers a real rollout
echo "Building images from main with tag '${MAIN_TAG}'"
make docker-build IMAGE_TAG="${MAIN_TAG}"

# Load images into kind and deploy experimental manifests from main
echo "Loading images and deploying experimental manifests from main"
make kind-load IMAGE_TAG="${MAIN_TAG}"
make kind-deploy \
    SOURCE_MANIFEST=manifests/experimental.yaml \
    MANIFEST=operator-controller-experimental.yaml \
    HELM_SETTINGS="options.operatorController.deployment.image=${OPCON_IMAGE_REPO}:${MAIN_TAG} options.catalogd.deployment.image=${CATD_IMAGE_REPO}:${MAIN_TAG}"
