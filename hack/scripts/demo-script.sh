#!/usr/bin/env bash

trap "trap - SIGTERM && kill -- -$$" SIGINT SIGTERM EXIT
# Welcome to the catalogd demo
make kind-cluster-cleanup
kind delete cluster
make kind-cluster
kubectl cluster-info --context kind-catalogd
sleep 10
make install
sleep 10
# inspect crds (catalog)
kubectl get crds -A

# create a catalog
kubectl apply -f config/samples/core_v1alpha1_catalog.yaml
# shows catalog-sample
kubectl get catalog -A 
# waiting for catalog to report ready status
time kubectl wait --for=condition=Unpacked catalog/operatorhubio --timeout=1m

# port forward the catalogd-catalogserver service to interact with the HTTP server serving catalog contents
(kubectl -n catalogd-system port-forward svc/catalogd-catalogserver 8080:80)&

# check what 'packages' are available in this catalog
curl http://localhost:8080/catalogs/operatorhubio/all.json | jq -s '.[] | select(.schema == "olm.package") | .name'
# check what channels are included in the wavefront package
curl http://localhost:8080/catalogs/operatorhubio/all.json | jq -s '.[] | select(.schema == "olm.channel") | select(.package == "wavefront") | .name'
# check what bundles are included in the wavefront package
curl http://localhost:8080/catalogs/operatorhubio/all.json | jq -s '.[] | select(.schema == "olm.bundle") | select(.package == "wavefront") | .name'

