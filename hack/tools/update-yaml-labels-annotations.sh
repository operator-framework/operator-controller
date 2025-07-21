#!/bin/bash

set -x

filelist=(
    helm/olmv1/base/catalogd/rbac/experimental/*.yaml
    helm/olmv1/base/catalogd/rbac/standard/*.yaml
    helm/olmv1/base/catalogd/webhook/experimental/*.yaml
    helm/olmv1/base/catalogd/webhook/standard/*.yaml
    helm/olmv1/base/operator-controller/rbac/experimental/*.yaml
    helm/olmv1/base/operator-controller/rbac/standard/*.yaml
)

for f in "${filelist[@]}"; do
    # Put in the temporary items
    yq -i '.metadata.annotations.replaceMe = "annotations"' "${f}"
    yq -i '.metadata.labels.replaceMe = "labels"' "${f}"
    # Replace with helm template - must be done last or yq will complain about the file format
    sed -i.bak 's/replaceMe: annotations/{{- include "olmv1.annoations" | nindent 4 }}/g' "${f}"
    sed -i.bak 's/replaceMe: labels/{{- include "olmv1.labels" | nindent 4 }}/g' "${f}"
    # Delete sed's backup file
    rm "${f}.bak"
done
