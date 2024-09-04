#!/usr/bin/env bash

set -e

# registry.sh will create an in-cluster image registry useful for end-to-end testing
# of catalogd's unpacking process. It does a few things:
# 1. Installs cert-manager for creating a self-signed certificate for the image registry
# 2. Creates all the resources necessary for deploying the image registry in the catalogd-e2e namespace
# 3. Creates ConfigMaps containing the test catalog + Dockerfile to be mounted to the kaniko pod
# 4. Waits for kaniko pod to have Condition Complete == true, indicating the test catalog image has been built + pushed
# to the test image registry
# Usage:
# registry.sh

# create the image registry with all the certs
kubectl apply -f test/tools/imageregistry/imgreg.yaml
kubectl wait -n catalogd-e2e --for=condition=Available deployment/docker-registry --timeout=60s

# Load the testdata onto the cluster as a configmap so it can be used with kaniko
kubectl create configmap -n catalogd-e2e --from-file=testdata/catalogs/test-catalog.Dockerfile catalogd-e2e.dockerfile
kubectl create configmap -n catalogd-e2e --from-file=testdata/catalogs/test-catalog catalogd-e2e.build-contents

# Create the kaniko pod to build the test image and push it to the test registry.
kubectl apply -f test/tools/imageregistry/imagebuilder.yaml
kubectl wait --for=condition=Complete -n catalogd-e2e jobs/kaniko --timeout=60s
