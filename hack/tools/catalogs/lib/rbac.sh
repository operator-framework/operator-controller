# Library of settings and helper functions for RBAC rule collection
# When generating the RBAC rules for the cluster extension installation, the rules are aggregated
# into two JSON arrays: one for cluster-scoped resources (CLUSTER_RBAC_RULES) and one for namespace-scoped resources (NAMESPACE_RBAC_RULES).

export CLUSTER_SCOPED_RESOURCES=(
    # handled separately
    # "ClusterRole"
    # "ClusterRoleBinding"
    "PriorityClass"
    "ConsoleYAMLSample"
    "ConsoleQuickStart"
    "ConsoleCLIDownload"
    "ConsoleLink"
)

export NAMESPACE_SCOPED_RESOURCES=(
    "Secret"
    "ConfigMap"
    # handled separately
    # "ServiceAccount"
    # "Service"
    "Role"
    "RoleBinding"
    "PrometheusRule"
    "ServiceMonitor"
    "PodDisruptionBudget"
    "VerticalPodAutoscaler"
)

# The allowed verbs for all CRDS
export ALL_RESOURCE_VERBS='"create", "list", "watch"'

# The allowed verbs for named CRDS
export NAMED_RESOURCE_VERBS='"get", "update", "patch", "delete"'

# Initialize variables to hold aggregated RBAC rules
CLUSTER_RBAC_RULES="[]"
NAMESPACE_RBAC_RULES="[]"

# Function to create a JSON-formatted RBAC rule
make_rbac_rule() {
    local apiGroups="${1}"
    local resources="${2}"
    local verbs="${3}"
    local resourceNames=("${@:4}")
    cat <<EOF
{
 "apiGroups": ["$apiGroups"],
 "resources": ["$resources"],
 "verbs": [${verbs}]$(if [ ${#resourceNames[@]} -gt 0 ]; then
    echo -n ',
 "resourceNames": [';
for c in "${resourceNames[@]}"; do
    echo -n "\"${c}\", ";
done | sed 's/, $//'
echo -n ']'
fi)
}
EOF
}

# Function to aggregate rules into a JSON array
aggregate_rules() {
    local rules="$1"
    local scope="$2"
    debug "rules: $(echo "${rules}" | jq -c)"
    if [ -n "$rules" ]; then
        for rule in $(echo "$rules" | jq -c '.'); do
            if [ "$scope" == "cluster" ]; then
                CLUSTER_RBAC_RULES=$(echo "$CLUSTER_RBAC_RULES" | jq --argjson new_rule "$rule" '. += [$new_rule]')
            else
                NAMESPACE_RBAC_RULES=$(echo "$NAMESPACE_RBAC_RULES" | jq --argjson new_rule "$rule" '. += [$new_rule]')
            fi
        done
    fi
}

# Utility to create and store RBAC rules for a given apiGroup/resource
# It creates two RBAC rules:
# 1. Valid for all resources on the cluster/namespace
# 2. Only valid for named resources on the cluster/namespace
# This is done to follow the general pattern of giving the installer service account
# permissions to "create" any resource of that type, but only allow CRUD on specific resources.
#
# For instance:
# rules:
# - apiGroups: [apiextensions.k8s.io]
#   resources: [customresourcedefinitions]
#   verbs: [create]
# - apiGroups: [apiextensions.k8s.io]
#   resources: [customresourcedefinitions]
#   verbs: [get, list, watch, update, patch, delete]
#   resourceNames:
#   - appprojects.argoproj.io
#   - argocds.argoproj.io
#   - applications.argoproj.io
#   - argocdexports.argoproj.io
#   - applicationsets.argoproj.io
add_rbac_rules() {
    local api_group="${1}"
    local resource="${2}"
    local scope="${3}"
    local all_resource_verbs="${4}" # all-resource verbs
    local named_resource_verbs="${5}" # named-resource verbs
    local resource_names=("${@:6}")

    local all_resource_perms
    all_resource_perms="$(make_rbac_rule "${api_group}" "${resource}" "${all_resource_verbs}")"

    local named_resource_perms
    named_resource_perms="$(make_rbac_rule "${api_group}" "${resource}" "${named_resource_verbs}" "${resource_names[@]}")"

    aggregate_rules "${all_resource_perms}" "${scope}"
    aggregate_rules "${named_resource_perms}" "${scope}"
}
