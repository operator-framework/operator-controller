#! /bin/bash

set -o errexit
set -o nounset
set -o pipefail

help="setup.sh is used to build operators using the operator-sdk and
build the image + bundle image, and create a FBC image for the
following bundle formats:
- registry+v1
- plain+v0
This script will ensure that all images built are loaded onto
a KinD cluster with the name specified in the arguments.
The following environment variables are required for configuring this script:
- \$CATALOG_IMG - the tag for the catalog image that contains the plain+v0 and registry+v1 operator bundle.
- \$REG_PKG_NAME - the name of the package for the operator that uses the registry+v1 bundle format.
- \$PLAIN_PKG_NAME - the name of the package for the operator that uses the plain+v0 bundle format.
setup.sh also takes 5 arguments. 

Usage:
  setup.sh [OPERATOR_SDK] [CONTAINER_RUNTIME] [KUSTOMIZE] [KIND] [KIND_CLUSTER_NAME]
"

########################################
# Input validation
########################################

if [[ "$#" -ne 5 ]]; then
  echo "Illegal number of arguments passed"
  echo "${help}"
fi

if [[ -z "${CATALOG_IMG}" ]]; then 
  echo "\$CATALOG_IMG is required to be set"
  echo "${help}"
fi

if [[ -z "${REG_PKG_NAME}" ]]; then 
  echo "\$REG_PKG_NAME is required to be set"
  echo "${help}"
fi

if [[ -z "${PLAIN_PKG_NAME}" ]]; then 
  echo "\$PLAIN_PKG_NAME is required to be set"
  echo "${help}"
fi

########################################
# Setup temp dir and local variables
########################################

# We're going to do file manipulation, so let's work in a temp dir
TMP_ROOT="$(mktemp -d ./tmp.XXXXXX)"
# Make sure to delete the temp dir when we exit
trap 'rm -rf ${TMP_ROOT}' EXIT

DOMAIN=oc-opdev-e2e.operatorframework.io
REG_DIR="${TMP_ROOT}/registry"
mkdir -p "${REG_DIR}"

PLAIN_DIR="${TMP_ROOT}/plain"
mkdir -p "${PLAIN_DIR}"

operator_sdk=$1
container_tool=$2
kustomize=$3
kind=$4
kcluster_name=$5

reg_img="${DOMAIN}/registry:v0.0.1"
reg_bundle_img="${DOMAIN}/registry-bundle:v0.0.1"
plain_img="${DOMAIN}/plain:v0.0.1"
plain_bundle_img="${DOMAIN}/plain-bundle:v0.0.1"

catalog_img="${CATALOG_IMG}"
reg_pkg_name="${REG_PKG_NAME}"
plain_pkg_name="${PLAIN_PKG_NAME}"

########################################
# Create the registry+v1 based operator
# and build + load images
########################################

(
  cd "${REG_DIR}" && \
  $operator_sdk init --domain="${DOMAIN}" && \
  $operator_sdk create api \
    --group="${DOMAIN}" \
    --version v1alpha1 \
    --kind Registry \
    --resource --controller && \
  make generate manifests && \
  make docker-build IMG="${reg_img}" && \
  sed -i -e 's/$(OPERATOR_SDK) generate kustomize manifests -q/$(OPERATOR_SDK) generate kustomize manifests -q --interactive=false/g' Makefile && \
  make bundle IMG="${reg_img}" VERSION=0.0.1 && \
  make bundle-build BUNDLE_IMG="${reg_bundle_img}"
)

$kind load docker-image "${reg_img}" --name "${kcluster_name}"
$kind load docker-image "${reg_bundle_img}" --name "${kcluster_name}"

#####################################
# Create the plain+v0 based operator
# and build + load images
#####################################

(
  cd "${PLAIN_DIR}" && \
  $operator_sdk init --domain="${DOMAIN}" && \
  $operator_sdk create api \
    --group="${DOMAIN}" \
    --version v1alpha1 \
    --kind Plain \
    --resource --controller && \
  make generate manifests && \
  make docker-build IMG="${plain_img}"
  mkdir -p manifests && \
  $kustomize build config/default > manifests/manifests.yaml
)

cat << EOF > "${PLAIN_DIR}"/plainbundle.Dockerfile
FROM scratch
ADD manifests /manifests
EOF

$container_tool build -t "${plain_bundle_img}" -f "${PLAIN_DIR}"/plainbundle.Dockerfile "${PLAIN_DIR}"/

$kind load docker-image "${plain_img}" --name "${kcluster_name}"
$kind load docker-image "${plain_bundle_img}" --name "${kcluster_name}"

#####################################
# Create the FBC that contains both 
# the plain+v0 and registry+v1 operators
#####################################

cat << EOF > "${TMP_ROOT}"/catalog.Dockerfile
FROM scratch
ADD catalog /configs
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
{
  "schema": "olm.package",
  "name": "${plain_pkg_name}"
}
{
  "schema": "olm.bundle",
  "name": "${plain_pkg_name}.v0.0.1",
  "package": "${plain_pkg_name}",
  "image": "$plain_bundle_img",
  "properties": [
    {
      "type": "olm.package",
      "value": {
        "packageName": "${plain_pkg_name}",
        "version": "0.0.1"
      }
    },
    {
      "type": "olm.bundle.mediatype",
      "value": "plain+v0"
    }
  ]
}
{
  "schema": "olm.channel",
  "name": "preview",
  "package": "${plain_pkg_name}",
  "entries": [
    {
      "name": "${plain_pkg_name}.v0.0.1"
    }
  ]
}
EOF

docker build -t "${catalog_img}" -f "${TMP_ROOT}"/catalog.Dockerfile "${TMP_ROOT}"/
$kind load docker-image "${catalog_img}" --name "${kcluster_name}"

# Make sure all files are removable. This is necessary because
# the Makefiles generated by the Operator-SDK have targets
# that install binaries under the bin/ directory. Those binaries
# don't have write permissions so they can't be removed unless
# we ensure they have the write permissions
chmod -R +w "${REG_DIR}/bin"
chmod -R +w "${PLAIN_DIR}/bin"