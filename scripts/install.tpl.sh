#!/bin/bash
set -euo pipefail
IFS=$'\n\t'

operator_controller_manifest=$MANIFEST

if [[ -z "$operator_controller_manifest" ]]; then
    echo "Error: Missing required MANIFEST variable"
    exit 1
fi

catalogd_manifest="./catalogd/catalogd.yaml"
default_catalogs_manifest="./catalogd/config/base/default/clustercatalogs/default-catalogs.yaml"
cert_mgr_version=$CERT_MGR_VERSION
install_default_catalogs=$INSTALL_DEFAULT_CATALOGS

if [[ ! -f "$catalogd_manifest" ]]; then
    echo "Error: Missing required catalogd manifest file at $catalogd_manifest"
    exit 1
fi

if [[ ! -f "$default_catalogs_manifest" ]]; then
    echo "Error: Missing required default catalogs manifest file at $default_catalogs_manifest"
    exit 1
fi

if [[ -z "$cert_mgr_version" ]]; then
    echo "Error: Missing CERT_MGR_VERSION variable"
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

function kubectl_wait_for_query() {
    manifest=$1
    query=$2
    timeout=$3
    poll_interval_in_seconds=$4

    if [[ -z "$manifest" || -z "$query" || -z "$timeout" || -z "$poll_interval_in_seconds" ]]; then
        echo "Error: Missing arguments."
        echo "Usage: kubectl_wait_for_query <manifest> <query> <timeout> <poll_interval_in_seconds>"
        exit 1
    fi

    start_time=$(date +%s)
    while true; do
        val=$(kubectl get "${manifest}" -o jsonpath="${query}" 2>/dev/null || echo "")
        if [[ -n "${val}" ]]; then
            echo "${manifest} has ${query}."
            break
        fi
        if [[ $(( $(date +%s) - start_time )) -ge ${timeout} ]]; then
            echo "Timed out waiting for ${manifest} to have ${query}."
            exit 1
        fi
        sleep ${poll_interval_in_seconds}s
    done
}

kubectl apply -f "https://github.com/cert-manager/cert-manager/releases/download/${cert_mgr_version}/cert-manager.yaml"
# Wait for cert-manager to be fully ready
kubectl_wait "cert-manager" "deployment/cert-manager-webhook" "60s"
kubectl_wait "cert-manager" "deployment/cert-manager-cainjector" "60s"
kubectl_wait "cert-manager" "deployment/cert-manager" "60s"
kubectl_wait_for_query "mutatingwebhookconfigurations/cert-manager-webhook" '{.webhooks[0].clientConfig.caBundle}' 60 5
kubectl_wait_for_query "validatingwebhookconfigurations/cert-manager-webhook" '{.webhooks[0].clientConfig.caBundle}' 60 5

kubectl apply -f "${catalogd_manifest}"
# Wait for the rollout, and then wait for the deployment to be Available
kubectl_wait_rollout "olmv1-system" "deployment/catalogd-controller-manager" "60s"
kubectl_wait "olmv1-system" "deployment/catalogd-controller-manager" "60s"

if [[ "${install_default_catalogs}" != "false" ]]; then
    kubectl apply -f "${default_catalogs_manifest}"
    kubectl wait --for=condition=Serving "clustercatalog/operatorhubio" --timeout="60s"
fi

kubectl apply -f "${operator_controller_manifest}"
kubectl_wait "olmv1-system" "deployment/operator-controller-controller-manager" "60s"
