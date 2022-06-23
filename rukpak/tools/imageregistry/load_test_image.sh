#!/usr/bin/env bash

export REGISTRY_NAME="docker-registry"
export REGISTRY_NAMESPACE=rukpak-e2e
export DNS_NAME=$REGISTRY_NAME.$REGISTRY_NAMESPACE.svc.cluster.local

# push test bundle image into in-cluster docker registry
kubectl exec nerdctl -n $REGISTRY_NAMESPACE -- sh -c "nerdctl login -u myuser -p mypasswd $DNS_NAME:5000 --insecure-registry"

for x in $(docker images --format "{{.Repository}}:{{.Tag}}" | grep testdata); do
    kubectl exec nerdctl -n $REGISTRY_NAMESPACE -- sh -c "nerdctl -n k8s.io tag $x $DNS_NAME:5000${x##testdata}"
    kubectl exec nerdctl -n $REGISTRY_NAMESPACE -- sh -c "nerdctl -n k8s.io push $DNS_NAME:5000${x##testdata} --insecure-registry"
    kubectl exec nerdctl -n $REGISTRY_NAMESPACE -- sh -c "nerdctl -n k8s.io rmi $DNS_NAME:5000${x##testdata} --insecure-registry"
done

