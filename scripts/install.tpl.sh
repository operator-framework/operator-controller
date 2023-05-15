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
kube_prometheus_version=$KUBE_PROMETHEUS_VERSION

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
    # We're ok with not installing kube-prometheus
    echo "$err"
    exit 1
fi

function kubectl_wait() {
    namespace=$1
    runtime=$2
    timeout=$3

    kubectl wait --for=condition=Available --namespace="${namespace}" "${runtime}" --timeout="${timeout}"
}

function get_git_repo() {
    local repo=$1
    local ver=$2
    local dest=$3
    if [[ -d "${dest}" ]]; then
        local cur=$(git -C ${dest} symbolic-ref -q --short HEAD)
        cur=${cur:-$(git -C ${dest} describe --tags --exact-match 2>/dev/null)}
        cur=${cur:-HEAD}
        if [[ "${cur}" != "${ver}" ]]; then
            rm -rf ${dest}
        fi
    fi
    if [[ ! -d "${dest}" ]]; then
        git clone "${repo}" "${dest}" --branch "${ver}" --depth 1 --quiet 2>/dev/null
    fi
}

echo -e "\nInstalling cert-manager ${cert_mgr_version}\n"
kubectl apply -f "https://github.com/cert-manager/cert-manager/releases/download/${cert_mgr_version}/cert-manager.yaml"
kubectl_wait "cert-manager" "deployment/cert-manager-webhook" "60s"

echo -e "\nInstalling rukpak ${rukpak_version}\n"
kubectl apply -f "https://github.com/operator-framework/rukpak/releases/download/${rukpak_version}/rukpak.yaml"
kubectl_wait "rukpak-system" "deployment/core" "60s"
kubectl_wait "rukpak-system" "deployment/helm-provisioner" "60s"
kubectl_wait "rukpak-system" "deployment/rukpak-webhooks" "60s"

if [[ -n "$kube_prometheus_version" ]]; then
    echo -e "\nInstalling kube-prometheus ${kube_prometheus_version}\n"
    get_git_repo "https://github.com/prometheus-operator/kube-prometheus.git" "${kube_prometheus_version}" bin/kube-prometheus
    kubectl apply --server-side -f bin/kube-prometheus/manifests/setup
    kubectl wait --for condition=Established --all CustomResourceDefinition --namespace=monitoring --timeout=60s
    kubectl apply -f bin/kube-prometheus/manifests/
fi

echo -e "\nInstalling catalogd ${catalogd_version}\n"
kubectl apply -f "https://github.com/operator-framework/catalogd/releases/download/${catalogd_version}/catalogd.yaml"
kubectl_wait "catalogd-system" "deployment/catalogd-controller-manager" "60s"

echo -e "\nInstalling operator-controller\n"
kubectl apply -f "${operator_controller_manifest}"
kubectl_wait "operator-controller-system" "deployment/operator-controller-controller-manager" "60s"
