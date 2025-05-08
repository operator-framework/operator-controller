#!/usr/bin/env bash

#
# Welcome to the OwnNamespace install mode demo
#
trap "trap - SIGTERM && kill -- -$$" SIGINT SIGTERM EXIT

# list namespaces
kubectl get ns

# show cluster extension definition
bat --style=plain hack/demo/resources/optional-sa/cluster-extension.yaml

# apply cluster extension
kubectl apply -f ${DEMO_RESOURCE_DIR}/optional-sa/cluster-extension.yaml

# wait for install to complete
kubectl wait clusterextension zookeeper-operator --for=condition=Installed=true

# see full cluster extension
kubectl get clusterextension zookeeper-operator -o yaml

# show deployment
kubectl get deployments -n zookeeper-operator
