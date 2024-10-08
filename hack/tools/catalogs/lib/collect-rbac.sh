# Library of functions for collecting RBAC from manifests for the purposes of generating
# The required RBAC for the cluster extension installation
source "$(dirname "${BASH_SOURCE[0]}")/utils.sh"
source "$(dirname "${BASH_SOURCE[0]}")/rbac.sh"
source "$(dirname "${BASH_SOURCE[0]}")/manifests.sh"
source "$(dirname "${BASH_SOURCE[0]}")/bundle.sh"

# Function to add the specified rules
add_required_rules() {
    local finalizer_perm
    finalizer_perm=$(make_rbac_rule "olm.operatorframework.io" "clusterextensions/finalizers" '"update"' "$EXTENSION_NAME")
    aggregate_rules "${finalizer_perm}" "cluster"
}

collect_crd_rbac() {
    debug "Collecting CRD permissions"
    local csv="${1}"
    crds=()
    while IFS=$'\n' read -r crd; do
         crds+=("$crd")
    done < <(yq eval -o=json -r '.spec.customresourcedefinitions.owned[]?.name' "$csv")
    add_rbac_rules "apiextensions.k8s.io" "customresourcedefinitions" "cluster" "${ALL_RESOURCE_VERBS}" "${NAMED_RESOURCE_VERBS}" "${crds[@]}"
}

collect_cluster_role_rbac() {
    local manifest_dir="${1}"
    local csv="${2}"
    debug "Adding ClusterRole RBAC"

    # Collect shipped ClusterRole names
    # And the OLMv1 generated ClusterRole names
    read -ra manifest_cluster_role_names <<< "$(collect_resource_names "${manifest_dir}" "ClusterRole")"
    read -ra generated_cluster_role_names <<< "$(generated_cluster_role_names "${csv}")"
    all_cluster_role_names=("${manifest_cluster_role_names[@]}" "${generated_cluster_role_names[@]}")
    add_rbac_rules "rbac.authorization.k8s.io" "clusterroles" "cluster" "${ALL_RESOURCE_VERBS}" "${NAMED_RESOURCE_VERBS}" "${all_cluster_role_names[@]}"

    # Add all rules for defined in shipped ClusterRoles
    # This allows the installer service account to grant rbac
    manifest_cr_perms="$(collect_manifest_cluster_role_perms "${manifest_dir}")"
    for item in $(echo "$manifest_cr_perms" | jq -c -r '.[]'); do
        aggregate_rules "${item}" "cluster"
    done

    debug "Adding ClusterPermissions"
    # Add all cluster scoped rules for defined in the CSV
    # This allows the installer service account to grant rbac
    cluster_permissions=$(yq eval -o=json '.spec.install.spec.clusterPermissions[].rules?' "$csv" | jq -c '.[]')
    for perm in ${cluster_permissions}; do
        aggregate_rules "${perm}" "cluster"
    done

    # Collect RBAC for cluster scoped manifest objects
    collect_cluster_scoped_resource_rbac "${manifest_dir}"

    debug "Adding ClusterRoleBinding RBAC"
    # Collect shipped ClusterRoleBinding names
    # And the OLMv1 generated ClusterRoleBinding names (same as the generated ClusterRole names)
    read -ra manifest_cluster_role_binding_names <<< "$(collect_resource_names "${manifest_dir}" "ClusterRoleBinding")"
    all_cluster_role_binding_names=("${manifest_cluster_role_binding_names[@]}" "${generated_cluster_role_names[@]}")
    add_rbac_rules "rbac.authorization.k8s.io" "clusterrolebindings" "cluster" "${ALL_RESOURCE_VERBS}" "${NAMED_RESOURCE_VERBS}" "${all_cluster_role_binding_names[@]}"
}

collect_cluster_scoped_resource_rbac() {
    debug "Adding other ClusterScoped Resources"
    local manifest_dir="${1}"
    for kind in "${CLUSTER_SCOPED_RESOURCES[@]}"; do
        read -ra resource_names <<< "$(collect_resource_names "${manifest_dir}" "${kind}")"
        if [ ${#resource_names[@]} -eq 0 ]; then
            continue
        fi
        api_group=$(get_api_group "${manifest_dir}" "${kind}" "${resource_names[1]}")
        add_rbac_rules "${api_group}" "${kind,,}s" "cluster" "${ALL_RESOURCE_VERBS}" "${NAMED_RESOURCE_VERBS}" "${resource_names[@]}"
    done
}

collect_operator_deployment_rbac() {
    local manifest_dir="${1}"
    local csv="${2}"

    debug "Adding Deployment RBAC"
    read -ra manifest_dep_names <<< "$(collect_resource_names "${manifest_dir}" "Deployment")"
    read -ra csv_deployments <<< "$(yq eval -o=json -r '.spec.install.spec.deployments[]?.name' "$csv")"
    all_deployments=("${manifest_dep_names[@]}" "${csv_deployments[@]}")
    add_rbac_rules "apps" "deployments" "namespace" "${ALL_RESOURCE_VERBS}" "${NAMED_RESOURCE_VERBS}" "${all_deployments[@]}"
}

collect_service_account_rbac() {
    debug "Adding ServiceAccount RBAC"
    local manifest_dir="${1}"
    local csv="${2}"
    read -ra manifest_sas <<< "$(collect_resource_names "${manifest_dir}" "ServiceAccount")"
    read -ra csv_sas <<< "$(yq eval '.. | select(has("serviceAccountName")) | .serviceAccountName' "${csv}" | sort -u)"
    all_sas=("${manifest_sas[@]}" "${csv_sas[@]}")
    add_rbac_rules "" "serviceaccounts" "namespace" "${ALL_RESOURCE_VERBS}" "${NAMED_RESOURCE_VERBS}" "${all_sas[@]}"
}

collect_role_rbac() {
    debug "Collecting Role RBAC"
    local manifest_dir="${1}"
    local csv="${2}"

    # Shipped Role manifest permissions
    manifest_role_perms="$(collect_manifest_role_perms "${manifest_dir}")"
    for item in $(echo "$manifest_role_perms" | jq -c -r '.[]'); do
        aggregate_rules "${item}" "namespace"
    done

    # CSV namespaced permissions
    namespace_permissions=$(yq eval -o=json '.spec.install.spec.permissions[].rules?' "$csv" | jq -c '.[]')
    for perm in ${namespace_permissions}; do
        aggregate_rules "${perm}" "cluster"
    done

    # Account for all other shipped namespace scoped resources
    for kind in "${NAMESPACE_SCOPED_RESOURCES[@]}"; do
        read -ra resource_names <<< "$(collect_resource_names "${manifest_dir}" "${kind}")"
        if [ ${#resource_names[@]} -eq 0 ]; then
            continue
        fi
        api_group=$(get_api_group "${manifest_dir}" "${kind}" "${resource_names[@]}")
        add_rbac_rules "${api_group}" "${kind,,}s" "namespace" "${ALL_RESOURCE_VERBS}" "${NAMED_RESOURCE_VERBS}" "${resource_names[@]}"
    done
}

# Expects a supported bundle
collect_installer_rbac() {
    local manifest_dir="${1}"
    csv="$(find_csv "${manifest_dir}")"

    echo "Collecting RBAC from bundle (manifest_dir:${manifest_dir} csv: ${csv})" >&2

    # Ensure bundle is supported by olmv1
    assert-bundle-supported "${manifest_dir}"

    # Add the required permissions rules
    add_required_rules

    # Grab CSV name
    collect_crd_rbac "${csv}"
    collect_cluster_role_rbac "${manifest_dir}" "${csv}"
    collect_operator_deployment_rbac "${manifest_dir}" "${csv}"
    collect_service_account_rbac "${manifest_dir}" "${csv}"
    collect_role_rbac "${manifest_dir}" "${csv}"
}
