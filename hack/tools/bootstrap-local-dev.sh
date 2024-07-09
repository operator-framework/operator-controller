#!/usr/bin/env bash

#######################################################################################################################
# This script bootstraps the local development environment for IDE-based development                                  #
# It will, assuming a kind cluster is running with the operator-controller deployed:                                  #
#   - copy the necessary service certs to the local machine                                                           #
#   - scale down the operator-controller deployment to 0 replicas                                                     #
#   - start kubefwd to forward the necessary services to the local machine                                            #
#                                                                                                                     #
# This is an alternative to using tilt (see tilt.md on the root of the repo)                                          #
#######################################################################################################################

#######################################################################################################################
# Note: this script relies on tools from bingo. If you have issues of the type "executable not found", run bingo get. #
#######################################################################################################################

ROOT_DIR=$(readlink -f "$(dirname "${BASH_SOURCE[0]}")/../..")

# Source tools
source "${ROOT_DIR}/.bingo/variables.env"

KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME:-operator-controller}"
OPERATOR_CONTROLLER_NAMESPACE="${OPERATOR_CONTROLLER_NAMESPACE:-olmv1-system}"
OPERATOR_CONTROLLER_DEPLOYMENT="${OPERATOR_CONTROLLER_DEPLOYMENT:-operator-controller-controller-manager}"
export KUBECONFIG="${KUBECONFIG:-$HOME/.kube/config}"
ENVTEST_VERSION="$(go list -m -modfile=${ROOT_DIR}/go.mod k8s.io/client-go | cut -d" " -f2 | sed 's/^v0\.\([[:digit:]]\{1,\}\)\.[[:digit:]]\{1,\}$$/1.\1.x/')"

# From root
echo "Setting kubeconfig to kind cluster"
${KIND} export kubeconfig --name "${KIND_CLUSTER_NAME}" > /dev/null 2>&1
echo "Scaling down operator-controller-controller-manager deployment"
eval "$(${SETUP_ENVTEST} use -p env "${ENVTEST_VERSION}" ${SETUP_ENVTEST_BIN_DIR_OVERRIDE})" && kubectl scale deployment -n "${OPERATOR_CONTROLLER_NAMESPACE}" "${OPERATOR_CONTROLLER_DEPLOYMENT}" --replicas=0
echo "Copying operator-controller system namespace service certificates from kind cluster"
echo "set --ca-certs-dir=$(./hack/tools/copy-service-certs.sh)"
echo "Starting kubefwd - sudo is needed for this"
sudo -E "${KUBEFWD}" svc -n "${OPERATOR_CONTROLLER_NAMESPACE}"
