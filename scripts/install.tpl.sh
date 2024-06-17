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

kubectl apply -f "https://github.com/cert-manager/cert-manager/releases/download/${cert_mgr_version}/cert-manager.yaml"
kubectl_wait "cert-manager" "deployment/cert-manager-webhook" "60s"

# Create the self-signed certificate for the ClusterIssuer and the ClusterIssuer
kubectl apply -f - <<EOF
apiVersion: cert-manager.io/v1
kind: Issuer
metadata:
  name: self-sign-issuer
  namespace: cert-manager
spec:
  selfSigned: {}
---
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: olmv1-ca
  namespace: cert-manager
spec:
  isCA: true
  commonName: olmv1-ca
  secretName: olmv1-ca
  privateKey:
    algorithm: ECDSA
    size: 256
  issuerRef:
    name: self-sign-issuer
    kind: Issuer
    group: cert-manager.io
---
apiVersion: cert-manager.io/v1
kind: ClusterIssuer
metadata:
  name: olmv1-ca
spec:
  ca:
    secretName: olmv1-ca
EOF

kubectl apply -f "https://github.com/operator-framework/catalogd/releases/download/${catalogd_version}/catalogd.yaml"
kubectl_wait "olmv1-system" "deployment/catalogd-controller-manager" "60s"

kubectl apply -f "${operator_controller_manifest}"
kubectl_wait "olmv1-system" "deployment/operator-controller-controller-manager" "60s"
