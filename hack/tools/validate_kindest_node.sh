#!/bin/bash
# This script verifies that the version of kind used for testing uses a major.minor version of k8s that operator-controller does

# Extract the version of kind, by removing the "${GOBIN}/kind-" prefix
KIND=${KIND#${GOBIN}/kind-}

GOMODCACHE=$(go env GOMODCACHE)

REGEX='v[0-9]+\.[0-9]+'

# Get the version of the image from the local kind build
if [ -d "${GOMODCACHE}" ]; then
    KIND_VER=$(grep -Eo "${REGEX}" ${GOMODCACHE}/sigs.k8s.io/kind@${KIND}/pkg/apis/config/defaults/image.go)
fi

# Get the version of the image from github
if [ -z "${KIND_VER}" ]; then
    KIND_VER=$(curl -L -s https://github.com/kubernetes-sigs/kind/raw/refs/tags/${KIND}/pkg/apis/config/defaults/image.go | grep -Eo "${REGEX}")
fi

if [ -z "${KIND_VER}" ]; then
    echo "Unable to determine kindest/node version"
    exit 1
fi

# Compare the versions
if [ "${KIND_VER}" != "${K8S_VERSION}" ]; then
    echo "kindest/node:${KIND_VER} version does not match k8s ${K8S_VERSION}"
    exit 1
fi
exit 0
