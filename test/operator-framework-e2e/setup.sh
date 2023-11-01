#! /bin/bash

set -e

#################################################################
# setup.sh is used to build operators using the operator-sdk and
# build the image + bundle image, and create a FBC image for the
# following bundle formats:
# - registry+v1
# - plain+v0
# This script will ensure that all images built are loaded onto
# a KinD cluster with the name specified in the arguments.
# The following environment variables areused for configuring this script:
# - $REGISTRY_CTRL_IMG - the tag for the registry+v1 operator image. 
#     Defaults to "oc-opdev-e2e.operatorframework.io/registry:v0.0.1"
# - $REGISTRY_BUNDLE_IMG - the tag for the registry+v1 operator bundle image.
#     Defaults to "oc-opdev-e2e.operatorframework.io/registry-bundle:v0.0.1"
# - $PLAIN_CTRL_IMG - the tag for the plain+v0 operator image.
#     Defaults to "oc-opdev-e2e.operatorframework.io/registry:v0.0.1"
# - $PLAIN_BUNDLE_IMG - the tag for the plain+v0 operator bundle image.
#     Defaults to "oc-opdev-e2e.operatorframework.io/plain-bundle:v0.0.1"
# - $CATALOG_IMG - the tag for the catalog image that contains the plain+v0 and registry+v0 operator bundle.
#     Defaults to "oc-opdev-e2e.operatorframework.io/catalog:e2e"
# setup.sh also takes 4 arguments. Usage:
# setup.sh [OPERATOR_SDK] [CONTAINER_RUNTIME] [KUSTOMIZE] [KIND] [KIND_CLUSTER_NAME]
#################################################################

########################################
# Setup temp dir and local variables
########################################

# We're going to do file manipulation, so let's work in a temp dir
TMP_ROOT="$(mktemp -p . -d 2>/dev/null || mktemp -d ./tmpdir.XXXXXXX)"
# Make sure to delete the temp dir when we exit
trap 'rm -rf $TMP_ROOT' EXIT

DOMAIN=oc-opdev-e2e.operatorframework.io
REG_DIR="${TMP_ROOT}/registry"
mkdir -p "$REG_DIR"

PLAIN_DIR="${TMP_ROOT}/plain"
mkdir -p "$PLAIN_DIR"

operator_sdk=$1
container_tool=$2
kustomize=$3
kind=$4
kcluster_name=$5

reg_img=${REGISTRY_CTRL_IMG:-"$DOMAIN/registry:v0.0.1"}
reg_bundle_img=${REGISTRY_BUNDLE_IMG:-"$DOMAIN/registry-bundle:v0.0.1"}
plain_img=${PLAIN_CTRL_IMG:-"$DOMAIN/plain:v0.0.1"}
plain_bundle_img=${PLAIN_BUNDLE_IMG:-"$DOMAIN/plain-bundle:v0.0.1"}
catalog_img=${CATALOG_IMG:-"$DOMAIN/catalog:e2e"}

########################################
# Create the registry+v1 based operator
# and build + load images
########################################

(
  cd "$REG_DIR" && \
  $operator_sdk init --domain=$DOMAIN && \
  $operator_sdk create api \
    --group=$DOMAIN \
    --version v1alpha1 \
    --kind Registry \
    --resource --controller && \
  make generate manifests && \
  make docker-build IMG="$reg_img" && \
  sed -i -e 's/$(OPERATOR_SDK) generate kustomize manifests -q/$(OPERATOR_SDK) generate kustomize manifests -q --interactive=false/g' Makefile && \
  make bundle IMG="$reg_img" VERSION=0.0.1 && \
  make bundle-build BUNDLE_IMG="$reg_bundle_img"
)

$kind load docker-image "$reg_img" --name "$kcluster_name"
$kind load docker-image "$reg_bundle_img" --name "$kcluster_name"

#####################################
# Create the plain+v0 based operator
# and build + load images
#####################################

(
  cd "$PLAIN_DIR" && \
  $operator_sdk init --domain=$DOMAIN && \
  $operator_sdk create api \
    --group=$DOMAIN \
    --version v1alpha1 \
    --kind Plain \
    --resource --controller && \
  make generate manifests && \
  make docker-build IMG="$plain_img"
  mkdir -p manifests && \
  $kustomize build config/default > manifests/manifests.yaml
)

cat << EOF > "$PLAIN_DIR"/plainbundle.Dockerfile
FROM scratch
ADD manifests /manifests
EOF

$container_tool build -t "$plain_bundle_img" -f "$PLAIN_DIR"/plainbundle.Dockerfile "$PLAIN_DIR"/

$kind load docker-image "$plain_img" --name "$kcluster_name"
$kind load docker-image "$plain_bundle_img" --name "$kcluster_name"

#####################################
# Create the FBC that contains both 
# the plain+v0 and registry+v1 operators
#####################################

cat << EOF > "$TMP_ROOT"/catalog.Dockerfile
FROM scratch
ADD catalog /configs
EOF

mkdir -p "$TMP_ROOT/catalog"
cat <<EOF > "$TMP_ROOT"/catalog/index.yaml
{
  "schema": "olm.package",
  "name": "registry-operator"
}
{
  "schema": "olm.bundle",
  "name": "registry-operator.v0.0.1",
  "package": "registry-operator",
  "image": "$reg_bundle_img",
  "properties": [
    {
      "type": "olm.package",
      "value": {
        "packageName": "registry-operator",
        "version": "0.0.1"
      }
    }
  ]
}
{
  "schema": "olm.channel",
  "name": "preview",
  "package": "registry-operator",
  "entries": [
    {
      "name": "registry-operator.v0.0.1"
    }
  ]
}
{
  "schema": "olm.package",
  "name": "plain-operator"
}
{
  "schema": "olm.bundle",
  "name": "plain-operator.v0.0.1",
  "package": "plain-operator",
  "image": "$plain_bundle_img",
  "properties": [
    {
      "type": "olm.package",
      "value": {
        "packageName": "plain-operator",
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
  "package": "plain-operator",
  "entries": [
    {
      "name": "plain-operator.v0.0.1"
    }
  ]
}
EOF

docker build -t "$catalog_img" -f "$TMP_ROOT"/catalog.Dockerfile "$TMP_ROOT"/
$kind load docker-image "$catalog_img" --name "$kcluster_name"

# Make sure all files are removable. This is necessary because
# the Makefiles generated by the Operator-SDK have targets
# that install binaries under the bin/ directory. Those binaries
# don't have write permissions so they can't be removed unless
# we ensure they have the write permissions
chmod -R +w "$REG_DIR/bin"
chmod -R +w "$PLAIN_DIR/bin"