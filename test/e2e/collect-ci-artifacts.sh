#! /bin/bash

set -o pipefail
set -o nounset
set -o errexit

: "${KUBECONFIG:?}"
: "${ARTIFACT_DIR:?}"
: "${KUBECTL:=kubectl}"

function ensure_kubectl() {
    if ! which ${KUBECTL} &> /dev/null; then
        echo "cannot find kubectl binary in \$PATH"
        exit 1
    fi
}

function collect_artifacts() {
    commands=()
    commands+=("get operators.core.olm.io -o yaml")
    commands+=("get bundledeployments -o yaml")
    commands+=("get bundles -o yaml")

    echo "Storing the test artifact output in the ${ARTIFACT_DIR} directory"
    for command in "${commands[@]}"; do
        echo "Collecting ${command} output..."
        COMMAND_OUTPUT_FILE=${ARTIFACT_DIR}/${command// /_}

        ${KUBECTL} ${command} >> "${COMMAND_OUTPUT_FILE}"
    done
}

function main() {
    echo "Using the ${KUBECTL} kubectl binary"
    echo "Using the ${ARTIFACT_DIR} output directory"
    echo "Using the ${KUBECONFIG} kubeconfig"
    mkdir -p "${ARTIFACT_DIR}"

    ensure_kubectl
    collect_artifacts
}

main
