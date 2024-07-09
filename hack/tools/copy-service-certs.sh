#!/usr/bin/env bash

# Copies the certificate data from the operator-controller namespace to the local machine

set -o errexit
set -o pipefail

ROOT_DIR=$(readlink -f "$(dirname "${BASH_SOURCE[0]}")/..")
CERT_DIR="${ROOT_DIR}/certs"
OLMV1_NAMESPACE="olmv1-system"

rm -rf "${CERT_DIR}"
mkdir -p "${CERT_DIR}"
function import_certs() {
    local secret_name=$1
    data=$(kubectl get secret "${secret_name}" -n "${OLMV1_NAMESPACE}" -o jsonpath="{.data['tls\.crt']}")
    if [[ -n "${data}" ]]; then
      echo "${data}" | base64 --decode > "${CERT_DIR}/${secret_name}-tls.crt"
    fi
}

for secret in $(kubectl get secrets -n "${OLMV1_NAMESPACE}" -o jsonpath="{.items[*].metadata.name}"); do
    import_certs "${secret}"
done

echo "${CERT_DIR}"