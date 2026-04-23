#! /bin/bash

set -o errexit
set -o nounset
set -o pipefail

help="setup.sh is used to build extensions using the operator-sdk and
build the image + bundle image, and create a FBC image for the
following bundle formats:
- registry+v1
Images are built and tagged locally; pushing to the registry is
handled by the Go test code via crane + port-forward.
The following environment variables are required for configuring this script:
- \$OPERATOR_SDK - path to the operator-sdk binary.
- \$CONTAINER_RUNTIME - container runtime to use (e.g. docker, podman).
- \$CLUSTER_REGISTRY_HOST - registry address accessible from inside the cluster.
- \$CATALOG_TAG - OCI tag for the catalog image (e.g. e2e/test-catalog:v1).
- \$REG_PKG_NAME - the name of the package for the extension.

Usage:
  setup.sh
"

########################################
# Input validation
########################################

for var in OPERATOR_SDK CONTAINER_RUNTIME CLUSTER_REGISTRY_HOST CATALOG_TAG REG_PKG_NAME; do
  if [[ -z "${!var:-}" ]]; then
    echo "\$$var is required to be set"
    echo "${help}"
    exit 1
  fi
done

########################################
# Setup temp dir and local variables
########################################

# We're going to do file manipulation, so let's work in a temp dir
TMP_ROOT="$(mktemp -d ./tmp.XXXXXX)"
# Make sure to delete the temp dir when we exit
trap 'chmod -R +w ${TMP_ROOT} && rm -rf ${TMP_ROOT}' EXIT

DOMAIN=oc-opdev-e2e.operatorframework.io
REG_DIR="${TMP_ROOT}/registry"
mkdir -p "${REG_DIR}"

operator_sdk="${OPERATOR_SDK}"
container_tool="${CONTAINER_RUNTIME}"
cluster_registry_host="${CLUSTER_REGISTRY_HOST}"

catalog_tag="${cluster_registry_host}/${CATALOG_TAG}"
reg_pkg_name="${REG_PKG_NAME}"

reg_img="${DOMAIN}/registry:v0.0.1"
reg_bundle_img="${cluster_registry_host}/bundles/registry-v1/registry-bundle:v0.0.1"

########################################
# Create the registry+v1 based extension
# and build + load images
########################################

# controller-gen v0.13.0 (scaffolded by operator-sdk) panics when run with
# go 1.22, so pin to a more recent version.
# NOTE: This is a rough edge that users will experience

# The Makefile in the project scaffolded by operator-sdk uses an SDK binary
# in the path if it is present. Override via `export` to ensure we use
# the same version that we scaffolded with.
# NOTE: this is a rough edge that users will experience

(
  cd "${REG_DIR}" && \
  $operator_sdk init --domain="${DOMAIN}" && \
  sed -i -e 's/CONTROLLER_TOOLS_VERSION ?= v0.13.0/CONTROLLER_TOOLS_VERSION ?= v0.15.0/' Makefile && \
  $operator_sdk create api \
    --group="${DOMAIN}" \
    --version v1alpha1 \
    --kind Registry \
    --resource --controller && \
  export OPERATOR_SDK="${operator_sdk}" && \
  make generate manifests && \
  sed -i -e 's/$(CONTAINER_TOOL) build/$(CONTAINER_TOOL) build --provenance=false/' Makefile && \
  make docker-build IMG="${reg_img}" && \
  sed -i -e 's/$(OPERATOR_SDK) generate kustomize manifests -q/$(OPERATOR_SDK) generate kustomize manifests -q --interactive=false/g' Makefile && \
  make bundle IMG="${reg_img}" VERSION=0.0.1 && \
  make bundle-build BUNDLE_IMG="${reg_bundle_img}"
)

# Push is handled by the Go test via crane + port-forward,
# because docker push goes through the Docker daemon which
# may be in a different network context (e.g. colima VM).

###############################
# Create the FBC that contains
# the registry+v1 extensions
###############################

cat << EOF > "${TMP_ROOT}"/catalog.Dockerfile
FROM scratch
ADD catalog /configs
LABEL operators.operatorframework.io.index.configs.v1=/configs
EOF

mkdir -p "${TMP_ROOT}/catalog"
cat <<EOF > "${TMP_ROOT}"/catalog/index.yaml
{
  "schema": "olm.package",
  "name": "${reg_pkg_name}"
}
{
  "schema": "olm.bundle",
  "name": "${reg_pkg_name}.v0.0.1",
  "package": "${reg_pkg_name}",
  "image": "${reg_bundle_img}",
  "properties": [
    {
      "type": "olm.package",
      "value": {
        "packageName": "${reg_pkg_name}",
        "version": "0.0.1"
      }
    }
  ]
}
{
  "schema": "olm.channel",
  "name": "preview",
  "package": "${reg_pkg_name}",
  "entries": [
    {
      "name": "${reg_pkg_name}.v0.0.1"
    }
  ]
}
EOF

${container_tool} build --provenance=false -f "${TMP_ROOT}/catalog.Dockerfile" -t "${catalog_tag}" "${TMP_ROOT}/"

# Push is handled by the Go test via crane + port-forward.
