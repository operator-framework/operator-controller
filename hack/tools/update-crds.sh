#!/usr/bin/env bash
# This uses a custom CRD generator to create "standard" and "experimental" CRDs

# The names of the generated CRDs
CE="olm.operatorframework.io_clusterextensions.yaml"
CC="olm.operatorframework.io_clustercatalogs.yaml"

# order for modules and crds must match
# each item in crds must be unique, and should be associated with a module
modules=("operator-controller" "catalogd")
crds=("${CE}" "${CC}")

# Channels must much those in the generator
channels=("standard" "experimental")

# Create the temp output directories
CRD_TMP=$(mktemp -d)
for c in ${channels[@]}; do
    mkdir -p ${CRD_TMP}/${c}
done

# This calculates the controller-tools version, to keep the annotation correct
CONTROLLER_TOOLS_VER=$(go list -m sigs.k8s.io/controller-tools | awk '{print $2}')

# Generate the CRDs
go run ./hack/tools/crd-generator ${CRD_TMP} ${CONTROLLER_TOOLS_VER}

# Create the destination directories for each base/channel combo
for c in ${channels[@]}; do
    for b in ${modules[@]}; do
        mkdir -p config/base/${b}/crd/${c}
    done
done

# Copy the generated files
for b in ${!modules[@]}; do
    for c in ${channels[@]}; do
        cp ${CRD_TMP}/${c}/${crds[${b}]} config/base/${modules[${b}]}/crd/${c}
    done
done

# Clean up the temp output directories
rm -rf ${CRD_TMP}
