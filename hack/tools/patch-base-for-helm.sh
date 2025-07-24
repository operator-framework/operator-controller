#!/bin/bash

# This script patches the kubebuilder generated files to make them ready for helm
# The patching is done via a combination of `yq` to add valid YAML to the appropriate location
# and then `sed` is used to replace some text with Helm templating.
# This can't be done in one step because `yq` (or `kustomize` for that matter) can't manipulate
# YAML once helm templating has been added.

# Patch catalogd rbac
catalogd_rbac_filelist=(
    helm/olmv1/base/catalogd/rbac/experimental/*.yaml
    helm/olmv1/base/catalogd/rbac/standard/*.yaml
)
for f in "${catalogd_rbac_filelist[@]}"; do
    yq -i '.metadata.labels["app.kubernetes.io/name"] = "catalogd"' "${f}"
    yq -i 'with(.; select(.kind == "Role") | .rules += { "replaceMe": "catalogd-role-rules"})' "${f}"
    yq -i 'with(.; select(.kind == "ClusterRole") | .rules += { "replaceMe": "catalogd-cluster-role-rules"})' "${f}"
done

# Patch operator-controller rbac
operator_controller_rbac_filelist=(
    helm/olmv1/base/operator-controller/rbac/experimental/*.yaml
    helm/olmv1/base/operator-controller/rbac/standard/*.yaml
)
for f in "${operator_controller_rbac_filelist[@]}"; do
    yq -i '.metadata.labels["app.kubernetes.io/name"] = "operator-controller"' "${f}"
    yq -i 'with(.; select(.kind == "Role") | .rules += { "replaceMe": "operator-controller-role-rules"})' "${f}"
    yq -i 'with(.; select(.kind == "ClusterRole") | .rules += { "replaceMe": "operator-controller-cluster-role-rules"})' "${f}"
done

# Patch catalogd webhook
catalogd_webhook_filelist=(
    helm/olmv1/base/catalogd/webhook/experimental/*.yaml
    helm/olmv1/base/catalogd/webhook/standard/*.yaml
)
for f in "${catalogd_webhook_filelist[@]}"; do
    yq -i '.metadata.labels["app.kubernetes.io/name"] = "catalogd"' "${f}"
    yq -i '.metadata.name = "catalogd-mutating-webhook-configuration"' "${f}"
    yq -i '.metadata.annotations["catalogd-webhook-annotations"] = "replaceMe"' "${f}"
    yq -i '.webhooks[0].clientConfig.service.namespace = "olmv1-system"' "${f}"
    yq -i '.webhooks[0].clientConfig.service.name = "catalogd-service"' "${f}"
    yq -i '.webhooks[0].clientConfig.service.port = 9443' "${f}"
    yq -i '.webhooks[0].matchConditions[0].name = "MissingOrIncorrectMetadataNameLabel"' "${f}"
    yq -i '.webhooks[0].matchConditions[0].expression = "\"name\" in object.metadata && (!has(object.metadata.labels) || !(\"olm.operatorframework.io/metadata.name\" in object.metadata.labels) || object.metadata.labels[\"olm.operatorframework.io/metadata.name\"] != object.metadata.name)"' "${f}"
done

# Patch everything generically
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
    sed -i.bak 's/catalogd-webhook-annotations: replaceMe/{{- include "olmv1.catalogd.webhook.annotations" . | nindent 4 }}/g' "${f}"
    sed -i.bak 's/replaceMe: labels/{{- include "olmv1.labels" . | nindent 4 }}/g' "${f}"
    sed -i.bak 's/olmv1-system/{{ .Values.namespaces.olmv1.name }}/g' "${f}"
    sed -i.bak 's/- replaceMe: catalogd-role-rules/{{- include "olmv1.catalogd.role.rules" . | nindent 2 }}/g' "${f}"
    sed -i.bak 's/- replaceMe: catalogd-cluster-role-rules/{{- include "olmv1.catalogd.clusterRole.rules" . | nindent 2 }}/g' "${f}"
    sed -i.bak 's/- replaceMe: operator-controller-role-rules/{{- include "olmv1.operatorController.role.rules" . | nindent 2 }}/g' "${f}"
    sed -i.bak 's/- replaceMe: operator-controller-cluster-role-rules/{{- include "olmv1.operatorController.clusterRole.rules" . | nindent 2 }}/g' "${f}"
    # Delete sed's backup file
    rm -f "${f}.bak"
done
