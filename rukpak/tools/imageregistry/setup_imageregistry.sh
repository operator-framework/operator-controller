#!/usr/bin/env bash

export REGISTRY_NAME="docker-registry"
export REGISTRY_NAMESPACE=rukpak-e2e
export DNS_NAME=$REGISTRY_NAME.$REGISTRY_NAMESPACE.svc.cluster.local
export KIND_CLUSTER_NAME=$1

kubectl create ns $REGISTRY_NAMESPACE || true

# create self-signed certificate for registry server
mkdir -p /tmp/var/imageregistry/certs
openssl req -x509 -newkey rsa:4096 -days 365 -nodes -sha256 -keyout /tmp/var/imageregistry/certs/tls.key -out /tmp/var/imageregistry/certs/tls.crt -subj "/CN=$DNS_NAME" -addext "subjectAltName = DNS:$DNS_NAME"
kubectl create secret tls certs-secret --cert=/tmp/var/imageregistry/certs/tls.crt --key=/tmp/var/imageregistry/certs/tls.key -n $REGISTRY_NAMESPACE
kubectl create configmap trusted-ca -n $REGISTRY_NAMESPACE --from-file=ca.crt=/tmp/var/imageregistry/certs/tls.crt

# create image registry service
kubectl apply -f tools/imageregistry/service.yaml -n $REGISTRY_NAMESPACE

# set local variables
export REGISTRY_IP=$(kubectl get service $REGISTRY_NAME -n $REGISTRY_NAMESPACE -o jsonpath='{ .spec.clusterIP }')
export REGISTRY_PORT=5000

# Add ca certificate to Node
kubectl apply -f tools/imageregistry/daemonset.yaml -n $REGISTRY_NAMESPACE

# Add an entry in /etc/hosts of Node
docker exec $(docker ps | grep $KIND_CLUSTER_NAME'-control-plane' | cut -c 1-12) sh -c "/usr/bin/echo $REGISTRY_IP $DNS_NAME >>/etc/hosts"

sleep 5
# create image registry pod
kubectl apply -f tools/imageregistry/registry.yaml -n $REGISTRY_NAMESPACE

# create image upload  pod
kubectl apply -f tools/imageregistry/nerdctl.yaml -n $REGISTRY_NAMESPACE

# create imagePull secret for provisioner
export IMAGE_PULL_RECRET="registrysecret"
kubectl create ns rukpak-system || true
kubectl create secret docker-registry $IMAGE_PULL_RECRET --docker-server=$DNS_NAME:5000 --docker-username="myuser" --docker-password="mypasswd" --docker-email="email@foo.com" -n rukpak-system

echo #### Valiables ####
echo
echo REGISTRY_NAME     $REGISTRY_NAME
echo REGISTRY_IP       $REGISTRY_IP
echo REGISTRY_PORT     $REGISTRY_PORT
echo IMAGE_PULL_RECRET $IMAGE_PULL_RECRET

# clean up 
rm -rf /tmp/var/imageregistry/certs


