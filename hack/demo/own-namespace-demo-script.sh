#!/usr/bin/env bash

#
# Welcome to the OwnNamespace install mode demo
#
set -e
trap 'echo "Demo ran into error"; trap - SIGTERM && kill -- -$$; exit 1' ERR SIGINT SIGTERM EXIT

# install standard CRDs
echo "Install standard CRDs..."
kubectl apply -f "$(dirname "${BASH_SOURCE[0]}")/../../manifests/standard.yaml"

# wait for standard CRDs to be available
kubectl wait --for condition=established --timeout=60s crd/clusterextensions.olm.operatorframework.io

# Ensure controller is healthy
kubectl rollout status -n olmv1-system deployment/operator-controller-controller-manager

# create install namespace
kubectl create ns argocd-system

# create installer service account
kubectl create serviceaccount -n argocd-system argocd-installer

# give installer service account admin privileges (not for production environments)
kubectl create clusterrolebinding argocd-installer-crb --clusterrole=cluster-admin --serviceaccount=argocd-system:argocd-installer

# install cluster extension in own namespace install mode (watch-namespace == install namespace == argocd-system)
cat ${DEMO_RESOURCE_DIR}/own-namespace-demo.yaml

# apply cluster extension
kubectl apply -f ${DEMO_RESOURCE_DIR}/own-namespace-demo.yaml

# wait for cluster extension installation to succeed
kubectl wait --for=condition=Installed clusterextension/argocd-operator --timeout="60s"

# check argocd-operator controller deployment pod template olm.targetNamespaces annotation
kubectl get deployments -n argocd-system argocd-operator-controller-manager -o jsonpath="{.spec.template.metadata.annotations.olm\.targetNamespaces}"

# check for argocd-operator rbac in watch namespace
kubectl get roles,rolebindings -n argocd-system -o name

# get controller service-account name
kubectl get deployments -n argocd-system argocd-operator-controller-manager -o jsonpath="{.spec.template.spec.serviceAccount}"

# check service account for role binding is the same as controller service-account
rolebinding=$(kubectl get rolebindings -n argocd-system -o name | grep 'argocd-operator' | head -n 1)
kubectl get -n argocd-system $rolebinding -o jsonpath='{.subjects}' | jq .[0]

echo "Demo completed successfully!"

# cleanup resources
echo "Cleaning up demo resources..."
kubectl delete clusterextension argocd-operator --ignore-not-found=true
kubectl delete namespace argocd-system --ignore-not-found=true
kubectl delete clusterrolebinding argocd-installer-crb --ignore-not-found=true

# wait for operator-controller to become available with standard config
kubectl rollout status -n olmv1-system deployment/operator-controller-controller-manager

echo "Demo cleanup completed!"
