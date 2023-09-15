#!/usr/bin/env bash
# run against a local copy of https://github.com/grokspawn/asciidemo-tools
#  ../asciidemo-tools/generate-gif.sh hack/scripts/generate-asciidemo.sh docs/demo.gif

SCRIPTPATH="$( cd -- "$(dirname "$0")" > /dev/null 2>&1 ; pwd -P )"

ASCIIDEMO_TOOLS=../asciidemo-tools/demo-functions.sh
REPO="https://github.com/operator-framework/catalogd"

. $ASCIIDEMO_TOOLS

function run() {
    typeline -x  "# Welcome to the catalogd demo"
    typeline "make kind-cluster-cleanup"
    typeline "kind delete cluster"
    typeline "make kind-cluster"
    typeline "kubectl cluster-info --context kind-catalogd"
    sleep 10
    typeline "make install"
    sleep 10
    # inspect crds (catalog)
    typeline 'kubectl get crds -A'

    typeline -x "# create a catalog"
    typeline "kubectl apply -f config/samples/core_v1alpha1_catalog.yaml" # or other
    typeline "kubectl get catalog -A" # shows catalog-sample
    typeline -x "# waiting for catalog to report ready status"
    typeline "time kubectl wait --for=condition=Unpacked catalog/operatorhubio --timeout=1h"

    # port forward the catalogd-catalogserver service
    typeline -x "# port forward the catalogd-catalogserver service to interact with the HTTP server serving catalog contents"
    typline "kubectl -n catalogd-system port-forward svc/catalogd-catalogserver 8080:80"

    # inspect packages
    typeline -x "# check what 'packages' are available in this catalog"
    typeline "curl http://localhost:8080/catalogs/operatorhubio/all.json | jq -s '.[] | select(.schema == \"olm.package\") | .name'"
    # inspect channels
    typeline -x "# check what channels are included in the wavefront package"
    typeline "curl http://localhost:8080/catalogs/operatorhubio/all.json | jq -s '.[] | select(.schema == \"olm.channel\") | select(.package == \"wavefront\") | .name'"
    # inspect bundles
    typeline -x "# check what bundles are included in the wavefront package"
    typeline "curl http://localhost:8080/catalogs/operatorhubio/all.json | jq -s '.[] | select(.schema == \"olm.bundle\") | select(.package == \"ack-acm-controller\") | .name'"
}

run
