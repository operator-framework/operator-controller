#!/bin/bash

# This script patches the kubebuilder generated files to make them ready for helm
# The patching is done via a combination of `yq` to add valid YAML to the appropriate location
# and then `sed` is used to replace some text with Helm templating.
# This can't be done in one step because `yq` (or `kustomize` for that matter) can't manipulate
# YAML once helm templating has been added.

# Patch catalogd rbac

# Patch everything generically
filelist=(
    helm/olmv1/base/catalogd/crd/experimental/*.yaml
    helm/olmv1/base/catalogd/crd/standard/*.yaml
    helm/olmv1/base/operator-controller/crd/experimental/*.yaml
    helm/olmv1/base/operator-controller/crd/standard/*.yaml
)

for f in "${filelist[@]}"; do
    # Patch in the temporary items
    yq -i '.metadata.annotations.replaceMe = "annotations"' "${f}"
    yq -i '.metadata.labels.replaceMe = "labels"' "${f}"
    # Replace with helm template - must be done last or yq will complain about the file format
    sed -i.bak 's/replaceMe: annotations/{{- include "olmv1.annotations" . | nindent 4 }}/g' "${f}"
    sed -i.bak 's/replaceMe: labels/{{- include "olmv1.labels" . | nindent 4 }}/g' "${f}"
    # Delete sed's backup file
    rm -f "${f}.bak"
done
