#!/usr/bin/env bash

#
# Welcome to the OwnNamespace install mode demo
#
trap "trap - SIGTERM && kill -- -$$" SIGINT SIGTERM EXIT

# enable 'SingleOwnNamespaceInstallSupport' feature gate
kubectl patch deployment -n olmv1-system operator-controller-controller-manager --type='json' -p='[{"op": "add", "path": "/spec/template/spec/containers/0/args/-", "value": "--feature-gates=SingleOwnNamespaceInstallSupport=true"}]'

# wait for operator-controller to become available
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
