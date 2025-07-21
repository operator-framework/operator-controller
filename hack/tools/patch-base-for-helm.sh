#!/bin/bash

set -x

# Patch catalogd rbac
catalogd_rbac_filelist=(
    helm/olmv1/base/catalogd/rbac/experimental/*.yaml
    helm/olmv1/base/catalogd/rbac/standard/*.yaml
)
for f in "${catalogd_rbac_filelist[@]}"; do
    yq -i '.metadata.labels["app.kubernetes.io/name"] = "catalogd"' "${f}"
    rm "${f}.bak"
done

# Patch operator-controller rbac
operator_controller_rbac_filelist=(
    helm/olmv1/base/operator-controller/rbac/experimental/*.yaml
    helm/olmv1/base/operator-controller/rbac/standard/*.yaml
)
for f in "${operator_controller_rbac_filelist[@]}"; do
    yq -i '.metadata.labels["app.kubernetes.io/name"] = "operator-controller"' "${f}"
    rm "${f}.bak"
done

# Patch catalogd webhook
catalogd_webhook_filelist=(
    helm/olmv1/base/catalogd/webhook/experimental/*.yaml
    helm/olmv1/base/catalogd/webhook/standard/*.yaml
)
for f in "${catalogd_webhook_filelist[@]}"; do
    yq -i '.metadata.labels["app.kubernetes.io/name"] = "catalogd"' "${f}"
    yq -i '.metadata.name = "catalogd-mutating-webhook-configuration"' "${f}"
    # This really only applies to cert-manager configs, but it's an annotation
    yq -i '.metadata.annotations["cert-manager.io/inject-ca-from-secret"] = "cert-manager/olmv1-ca"' "${f}"
    yq -i '.webhooks[0].clientConfig.service.namespace = "olmv1-system"' "${f}"
    yq -i '.webhooks[0].clientConfig.service.name = "catalogd-service"' "${f}"
    yq -i '.webhooks[0].clientConfig.service.port = 9443' "${f}"
    yq -i '.webhooks[0].matchConditions[0].name = "MissingOrIncorrectMetadataNameLabel"' "${f}"
    yq -i '.webhooks[0].matchConditions[0].expression = "\"name\" in object.metadata && (!has(object.metadata.labels) || !(\"olm.operatorframework.io/metadata.name\" in object.metadata.labels) || object.metadata.labels[\"olm.operatorframework.io/metadata.name\"] != object.metadata.name)"' "${f}"
    rm "${f}.bak"
done

# Patch everything genericly
filelist=(
    helm/olmv1/base/catalogd/rbac/experimental/*.yaml
    helm/olmv1/base/catalogd/rbac/standard/*.yaml
    helm/olmv1/base/catalogd/crd/experimental/*.yaml
    helm/olmv1/base/catalogd/crd/standard/*.yaml
    helm/olmv1/base/catalogd/webhook/experimental/*.yaml
    helm/olmv1/base/catalogd/webhook/standard/*.yaml
    helm/olmv1/base/operator-controller/rbac/experimental/*.yaml
    helm/olmv1/base/operator-controller/rbac/standard/*.yaml
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
    sed -i.bak 's/olmv1-system/{{ .Values.namespaces.olmv1.name }}/g' "${f}"
    # Delete sed's backup file
    rm "${f}.bak"
done
