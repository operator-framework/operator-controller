#!/usr/bin/env bash

export REGISTRY_NAME="docker-registry"
export REGISTRY_NAMESPACE=rukpak-e2e
export DNS_NAME=$REGISTRY_NAME.$REGISTRY_NAMESPACE.svc.cluster.local
export KIND_CLUSTER_NAME=$1

# push test bundle image into in-cluster docker registry
kubectl exec nerdctl -n $REGISTRY_NAMESPACE -- sh -c "nerdctl login -u myuser -p mypasswd $DNS_NAME:5000 --insecure-registry"

docker build testdata/bundles/plain-v0/valid -t testdata/bundles/plain-v0:valid
kind load docker-image testdata/bundles/plain-v0:valid --name $KIND_CLUSTER_NAME
kubectl exec nerdctl -n $REGISTRY_NAMESPACE -- sh -c "nerdctl -n k8s.io tag testdata/bundles/plain-v0:valid $DNS_NAME:5000/bundles/plain-v0:valid"
kubectl exec nerdctl -n $REGISTRY_NAMESPACE -- sh -c "nerdctl -n k8s.io push $DNS_NAME:5000/bundles/plain-v0:valid --insecure-registry"
kubectl exec nerdctl -n $REGISTRY_NAMESPACE -- sh -c "nerdctl -n k8s.io rmi $DNS_NAME:5000/bundles/plain-v0:valid --insecure-registry"

# create bundle
kubectl apply -f tools/imageregistry/bundle_local_image.yaml
