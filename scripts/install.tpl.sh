#!/bin/bash
set -euo pipefail
IFS=$'\n\t'

operator_controller_manifest=$MANIFEST

if [[ -z "$operator_controller_manifest" ]]; then
    echo "Error: Missing required MANIFEST variable"
    exit 1
fi

catalogd_version=$CATALOGD_VERSION
cert_mgr_version=$CERT_MGR_VERSION
rukpak_version=$RUKPAK_VERSION

if [[ -z "$catalogd_version" || -z "$cert_mgr_version" || -z "$rukpak_version" ]]; then
    err="Error: Missing component version(s) for: "
    if [[ -z "$catalogd_version" ]]; then
        err+="catalogd "
    fi 
    if [[ -z "$cert_mgr_version" ]]; then
        err+="cert-manager "
    fi 
    if [[ -z "$rukpak_version" ]]; then
        err+="rukpak "
    fi 
    echo "$err"
    exit 1
fi

function kubectl_wait() {
    namespace=$1
    runtime=$2
    timeout=$3

    kubectl wait --for=condition=Available --namespace="${namespace}" "${runtime}" --timeout="${timeout}"
}

kubectl apply -f testdata/manifests/cert-manager-${cert_mgr_version}.yaml
kubectl_wait "cert-manager" "deployment/cert-manager-webhook" "60s"

kubectl apply -f testdata/manifests/rukpak-${rukpak_version}.yaml
kubectl_wait "rukpak-system" "deployment/core" "60s"
kubectl_wait "rukpak-system" "deployment/helm-provisioner" "60s"
kubectl_wait "rukpak-system" "deployment/rukpak-webhooks" "60s"

kubectl apply -f testdata/manifests/catalogd-${catalogd_version}.yaml
kubectl_wait "catalogd-system" "deployment/catalogd-controller-manager" "60s"

kubectl apply -f ${operator_controller_manifest}
kubectl_wait "operator-controller-system" "deployment/operator-controller-controller-manager" "60s"
