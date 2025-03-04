#!/usr/bin/env bash

#
# Welcome to the SingleNamespace install mode demo
#
trap "trap - SIGTERM && kill -- -$$" SIGINT SIGTERM EXIT

# enable 'SyntheticPermissions' feature
kubectl kustomize config/overlays/featuregate/synthetic-user-permissions | kubectl apply -f -

# wait for operator-controller to become available
kubectl rollout status -n olmv1-system deployment/operator-controller-controller-manager

# create install namespace
kubectl create ns argocd-system

# give cluster extension group cluster admin privileges - all cluster extensions installer users will be cluster admin
bat --style=plain ${DEMO_RESOURCE_DIR}/synthetic-user-perms/cegroup-admin-binding.yaml

# apply cluster role binding
kubectl apply -f ${DEMO_RESOURCE_DIR}/synthetic-user-perms/cegroup-admin-binding.yaml

# install cluster extension - for now .spec.serviceAccount = "olm.synthetic-user"
bat --style=plain ${DEMO_RESOURCE_DIR}/synthetic-user-perms/argocd-clusterextension.yaml

# apply cluster extension
kubectl apply -f ${DEMO_RESOURCE_DIR}/synthetic-user-perms/argocd-clusterextension.yaml

# wait for cluster extension installation to succeed
kubectl wait --for=condition=Installed clusterextension/argocd-operator --timeout="60s"
