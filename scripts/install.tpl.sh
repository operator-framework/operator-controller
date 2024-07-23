#!/bin/bash
set -euo pipefail
IFS=$'\n\t'

catalogd_manifest=$MANIFEST

if [[ -z "$catalogd_manifest" ]]; then
    echo "Error: Missing required MANIFEST variable"
    exit 1
fi

cert_mgr_version=$CERT_MGR_VERSION
default_catalogs=$DEFAULT_CATALOGS

if [[ -z "$default_catalogs" || -z "$cert_mgr_version" ]]; then
    err="Error: Missing component value(s) for: "
    if [[ -z "$default_catalogs" ]]; then
        err+="default cluster catalogs "
    fi 
    if [[ -z "$cert_mgr_version" ]]; then
        err+="cert-manager version "
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

kubectl apply -f "https://github.com/cert-manager/cert-manager/releases/download/${cert_mgr_version}/cert-manager.yaml"
kubectl_wait "cert-manager" "deployment/cert-manager-webhook" "60s"

kubectl apply -f "${catalogd_manifest}"
kubectl_wait "olmv1-system" "deployment/catalogd-controller-manager" "60s"

kubectl apply -f "${default_catalogs}"
kubectl wait --for=condition=Unpacked "clustercatalog/operatorhubio" --timeout="60s"