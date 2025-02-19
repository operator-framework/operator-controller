#!/usr/bin/env bash
#
# Welcome to the catalogd metas API endpoint demo
#
trap 'trap - SIGTERM && kill -- -"$$"' SIGINT SIGTERM EXIT

kind delete cluster
kind create cluster
kubectl cluster-info --context kind-kind
sleep 10

# use the install script from the latest github release
curl -L -s https://github.com/operator-framework/operator-controller/releases/latest/download/install.sh | bash

# inspect crds (clustercatalog)
kubectl get crds -A
kubectl get clustercatalog -A

# ... checking catalogd controller is available
kubectl wait --for=condition=Available -n olmv1-system deploy/catalogd-controller-manager --timeout=1m

# patch the deployment to include the feature gate
kubectl patch -n olmv1-system deploy/catalogd-controller-manager --type='json' -p='[{"op": "add", "path": "/spec/template/spec/containers/0/args/-", "value": "--feature-gates=APIV1MetasHandler=true"}]'

# ... waiting for new deployment for catalogd controller to become available
kubectl rollout status -n olmv1-system deploy/catalogd-controller-manager
# ... checking clustercatalog is serving
kubectl wait --for=condition=Serving clustercatalog/operatorhubio --timeout=60s
# ... checking clustercatalog is finished unpacking
kubectl wait --for=condition=Progressing=False clustercatalog/operatorhubio --timeout=60s


# port forward the catalogd-service service to interact with the HTTP server serving catalog contents
(kubectl -n olmv1-system port-forward svc/catalogd-service 8081:443)&

sleep 3

# check what 'packages' are available in this catalog
curl -vvv -k 'https://localhost:8081/catalogs/operatorhubio/api/v1/metas?schema=olm.package' | jq -s '.[] | .name'
# check what channels are included in the wavefront package
curl -vvv -k 'https://localhost:8081/catalogs/operatorhubio/api/v1/metas?schema=olm.channel&package=wavefront' | jq -s '.[] | .name'
# check what bundles are included in the wavefront package
curl -vvv -k 'https://localhost:8081/catalogs/operatorhubio/api/v1/metas?schema=olm.bundle&package=wavefront' | jq -s '.[] | .name'

