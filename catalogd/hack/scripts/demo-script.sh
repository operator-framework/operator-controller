#!/usr/bin/env bash

#
# Welcome to the catalogd demo
#
trap "trap - SIGTERM && kill -- -$$" SIGINT SIGTERM EXIT


kind delete cluster
kind create cluster
kubectl cluster-info --context kind-kind
sleep 10

# use the install script from the latest github release
curl -L -s https://github.com/operator-framework/catalogd/releases/latest/download/install.sh | bash

# inspect crds (clustercatalog)
kubectl get crds -A
kubectl get clustercatalog -A

echo "... checking catalogd controller is available"
kubectl wait --for=condition=Available -n olmv1-system deploy/catalogd-controller-manager --timeout=1m
echo "... checking clustercatalog is serving"
kubectl wait --for=condition=Serving clustercatalog/operatorhubio --timeout=60s
echo "... checking clustercatalog is finished unpacking"
kubectl wait --for=condition=Progressing=False clustercatalog/operatorhubio --timeout=60s

# port forward the catalogd-service service to interact with the HTTP server serving catalog contents
(kubectl -n olmv1-system port-forward svc/catalogd-service 8081:443)&

sleep 3

# check what 'packages' are available in this catalog
curl -k https://localhost:8081/catalogs/operatorhubio/api/v1/all | jq -s '.[] | select(.schema == "olm.package") | .name'
# check what channels are included in the wavefront package
curl -k https://localhost:8081/catalogs/operatorhubio/api/v1/all | jq -s '.[] | select(.schema == "olm.channel") | select(.package == "wavefront") | .name'
# check what bundles are included in the wavefront package
curl -k https://localhost:8081/catalogs/operatorhubio/api/v1/all | jq -s '.[] | select(.schema == "olm.bundle") | select(.package == "wavefront") | .name'

