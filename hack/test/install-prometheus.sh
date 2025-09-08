#!/bin/bash

source ".bingo/variables.env"

set -euo pipefail

help="install-prometheus.sh is used to set up prometheus monitoring for e2e testing.
Usage:
  install-prometheus.sh [PROMETHEUS_NAMESPACE] [PROMETHEUS_VERSION] [GIT_VERSION]
"

if [[ "$#" -ne 3 ]]; then
  echo "Illegal number of arguments passed"
  echo "${help}"
  exit 1
fi

PROMETHEUS_NAMESPACE="$1"
PROMETHEUS_VERSION="$2"
GIT_VERSION="$3"

TMPDIR="$(mktemp -d)"
trap 'echo "Cleaning up $TMPDIR"; rm -rf "$TMPDIR"' EXIT

echo "Downloading Prometheus resources..."
curl -s "https://raw.githubusercontent.com/prometheus-operator/prometheus-operator/refs/tags/${PROMETHEUS_VERSION}/kustomization.yaml" > "${TMPDIR}/kustomization.yaml"
curl -s "https://raw.githubusercontent.com/prometheus-operator/prometheus-operator/refs/tags/${PROMETHEUS_VERSION}/bundle.yaml" > "${TMPDIR}/bundle.yaml"

echo "Patching namespace to ${PROMETHEUS_NAMESPACE}..."
(cd "$TMPDIR" && ${KUSTOMIZE} edit set namespace "$PROMETHEUS_NAMESPACE")

echo "Applying Prometheus base..."
kubectl apply -k "$TMPDIR" --server-side

echo "Waiting for Prometheus Operator pod to become ready..."
kubectl wait --for=condition=Ready pod -n "$PROMETHEUS_NAMESPACE" -l app.kubernetes.io/name=prometheus-operator

echo "Applying prometheus Helm chart..."
${HELM} template prometheus helm/prometheus | sed "s/cert-git-version/cert-${VERSION}/g" | kubectl apply -f -

echo "Waiting for metrics scraper to become ready..."
kubectl wait --for=create pods -n "$PROMETHEUS_NAMESPACE" prometheus-prometheus-0 --timeout=60s
kubectl wait --for=condition=Ready pods -n "$PROMETHEUS_NAMESPACE" prometheus-prometheus-0 --timeout=120s

echo "Prometheus deployment completed successfully."
