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
install_default_catalogs=$INSTALL_DEFAULT_CATALOGS

if [[ -z "$catalogd_version" || -z "$cert_mgr_version" ]]; then
    err="Error: Missing component version(s) for: "
    if [[ -z "$catalogd_version" ]]; then
        err+="catalogd "
    fi 
    if [[ -z "$cert_mgr_version" ]]; then
        err+="cert-manager "
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

function kubectl_wait_rollout() {
    namespace=$1
    runtime=$2
    timeout=$3

    kubectl rollout status --namespace="${namespace}" "${runtime}" --timeout="${timeout}"
}

kubectl apply -f "https://github.com/cert-manager/cert-manager/releases/download/${cert_mgr_version}/cert-manager.yaml"
kubectl_wait "cert-manager" "deployment/cert-manager-webhook" "60s"

kubectl apply -f "https://github.com/operator-framework/catalogd/releases/download/${catalogd_version}/catalogd.yaml"
# Wait for the rollout, and then wait for the deployment to be Available
kubectl_wait_rollout "olmv1-system" "deployment/catalogd-controller-manager" "60s"
kubectl_wait "olmv1-system" "deployment/catalogd-controller-manager" "60s"

if [[ "${install_default_catalogs,,}" != "false" ]]; then
    kubectl apply -f "https://github.com/operator-framework/catalogd/releases/download/${catalogd_version}/default-catalogs.yaml"
    kubectl wait --for=condition=Unpacked "clustercatalog/operatorhubio" --timeout="60s"
fi

kubectl apply -f "${operator_controller_manifest}"
kubectl_wait "olmv1-system" "deployment/operator-controller-controller-manager" "60s"
