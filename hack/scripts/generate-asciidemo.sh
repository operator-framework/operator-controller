#!/usr/bin/env bash
## run against a local copy of https://github.com/grokspawn/asciidemo-tools
#  export TMPDIR=~/tmp/   # if linux, since docker doesn't like remodeling /tmp/ pathing
#  ./asciidemo-tools/generate-gif.sh ./generate-asciidemo.sh demo.gif

SCRIPTPATH="$( cd -- "$(dirname "$0")" > /dev/null 2>&1 ; pwd -P )"

ASCIIDEMO_TOOLS=./asciidemo-tools/demo-functions.sh
REPO="https://github.com/operator-framework/catalogd"

. $ASCIIDEMO_TOOLS

function run() {
    if [ -e ./catalogd ] ; then echo "ERROR:  ./catalogd exists.  Please delete before running this script."; exit 1; fi
    typeline -x  "# Welcome to the catalogd demo"
    typeline "git clone $REPO"
    typeline "cd catalogd"
    typeline "make kind-cluster"
    typeline "kubectl cluster-info --context kind-catalogd"
    sleep 10
    typeline "make install"
    sleep 10
    # inspect crds (catalogsource, package, bundlemetadata)
    #k get crds catalogsources.catalogd.operatorframework.io
    #k get crds packages.catalogd.operatorframework.io
    #k get crds bundlemetadata.catalogd.operatorframework.io
    #typeline 'kubectl get crds -A| grep -A10 -B10 -E "catalogsources|packages|bundlemetadata"'
    typeline 'kubectl get crds -A'

    typeline -x "# create a catalogsource"
    typeline "kubectl apply -f config/samples/core_v1beta1_catalogsource.yaml" # or other
    typeline "kubectl get catalogsource -A" # shows catalogsource-sample
    typeline -x "# waiting for catalogsource to report ready status"
    typeline "kubectl wait --for=condition=Ready catalogsource/catalogsource-sample --timeout=1h"
    # inspect packages, and then details on one package CR
    typeline -x "# check what 'packages' are available in this catalog and then inspect the content of one of the packages"
    typeline "kubectl get packages"
    typeline "kubectl get packages wavefront -o yaml"
    # inspect bundlemetadata, and then details on one bundlemetadata CR
    typeline -x "# check what bundles are included in those packages and then inspect the content of the wavefront-operator.v0.1.0 bundle included in the 'wavefront' package we just inspected"
    typeline "kubectl get bundlemetadata"
    typeline "kubectl get bundlemetadata wavefront-operator.v0.1.0 -o yaml"
}

run

