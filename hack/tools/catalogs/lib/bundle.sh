# Library of functions for interacting with bundles and their manifests

# SCRIPT_ROOT is the root of the script
source "$(dirname "${BASH_SOURCE[0]}")/hash.sh"

# Given package and version grabs the bundle image from stdin FBC stream
function get-bundle-image(){
    local package_name="${1}"
    local package_version="${2}"
    local image
    image=$(jq -r --arg pkg "$package_name" --arg ver "$package_version" \
        'select(.schema == "olm.bundle" and (.properties[] | select(.type == "olm.package" and .value.packageName == $pkg and .value.version == $ver))) | .image')

    if [ -z "$image" ]; then
        echo "ERROR: No matching image found for package '$package_name' with version '$package_version'." >&2
        exit 1
    fi

    echo "${image}"
}

function is-all-namespace-mode-enabled() {
    local csv="${1}"
    local valid
    valid=$(yq eval '(.spec.installModes[] | select(.type == "AllNamespaces" and .supported == true)) and true' "${csv}")
    echo "${valid}"
}

function does-not-have-webhooks() {
    local csv="${1}"
    local valid
    valid=$(yq eval '((.spec.webhookdefinitions == null) or (.spec.webhookdefinitions | length == 0))' "${csv}")
    echo "${valid}"
}

function does-not-have-dependencies() {
    local csv="${1}"
    local valid
    valid=$(yq eval '((.spec.customresourcedefinitions.required == null) or (.spec.customresourcedefinitions.required | length == 0))' "${csv}")
    echo "${valid}"
}

function is-crd-version-supported() {
    local manifest_dir="${1}"
    local valid=true
    while IFS= read -r resource_file; do
        version=$(yq eval '.apiVersion' "$resource_file")
        if [ "${version}" != "apiextensions.k8s.io/v1" ]; then
            valid="${version}"
            break
        fi
    done < <(find "${manifest_dir}" -type f -exec grep -l "^kind: CustomResourceDefinition" {} \;)
    echo "$valid"
}

function is-bundle-supported() {
    local manifest_dir="${1}"
    csv="$(find_csv "${manifest_dir}")"

    crd_version="$(is-crd-version-supported "${manifest_dir}")"

    if [ "$(is-all-namespace-mode-enabled "${csv}")" != "true" ]; then
        echo "Bundle not supported: AllNamespaces install mode is disabled" >&2
        echo "false"
    elif [ "$(does-not-have-webhooks "${csv}")" != "true" ]; then
        echo "Bundle not supported: contains webhooks" >&2
        echo "false"
    elif [ "$(does-not-have-dependencies "${csv}")" != "true" ]; then
        echo "Bundle not supported: contains dependencies" >&2
        echo "false"
    elif [ "${crd_version}" != "true" ]; then
        echo "Bundle not supported: unsupported CRD api version (${crd_version})" >&2
        echo "false"
    fi

    echo "true"
}

# Function to validate the bundle is supported
function assert-bundle-supported() {
    local manifest_dir="${1}"
    if [ "$(is-bundle-supported "${manifest_dir}")" != "true" ]; then
      exit 1
    fi
}

# Function to get all resource names for a particular kind
# from the manifest directory
function collect_resource_names() {
    local manifest_dir="${1}"
    local kind="${2}"
    local resource_names=()
    while IFS= read -r resource_file; do
        name=$(yq eval -r '.metadata.name' "$resource_file")
        if [ -n "$name" ]; then
            resource_names+=("$name")
        fi
    done < <(find "${manifest_dir}" -type f -exec grep -l "^kind: ${kind}" {} \;)
    echo "${resource_names[@]}"
}

# Function that collects all the rules for all the ClusterRole manifests
# shipped with the bundle
function collect_manifest_cluster_role_perms() {
    local manifest_dir="${1}"
    local kind="ClusterRole"
    local all_cr_rules="[]"

    while IFS= read -r resource_file; do
        # Extract the entire rules array from the current file and ensure it's treated as a valid JSON array
        cr_rules=$(yq eval -o=json -r '.rules // []' "$resource_file")
        # Validate and merge the current rules array with the cumulative rules array
        if jq -e . >/dev/null 2>&1 <<<"$cr_rules"; then
            all_cr_rules=$(jq -c --argjson existing "$all_cr_rules" --argjson new "$cr_rules" '$existing + $new' <<<"$all_cr_rules")
        fi
    done < <(find "${manifest_dir}" -type f -exec grep -l "^kind: ${kind}" {} \;)
    echo "$all_cr_rules"
}

# Function that collects all the rules for all the Role manifests
# shipped with the bundle
function collect_manifest_role_perms() {
    local manifest_dir="${1}"
    local kind="Role"
    local all_cr_rules="[]"

    while IFS= read -r resource_file; do
        # Extract the entire rules array from the current file and ensure it's treated as a valid JSON array
        cr_rules=$(yq eval -o=json -r '.rules // []' "$resource_file")
        # Validate and merge the current rules array with the cumulative rules array
        if jq -e . >/dev/null 2>&1 <<<"$cr_rules"; then
            all_cr_rules=$(jq -c --argjson existing "$all_cr_rules" --argjson new "$cr_rules" '$existing + $new' <<<"$all_cr_rules")
        fi
    done < <(find "${manifest_dir}" -type f -exec grep -l "^kind: ${kind}" {} \;)
    echo "$all_cr_rules"
}

# Function to get the apiGroup for a named resource of a given kind
# from the manifests dir
function get_api_group() {
    local dir_path="$1"
    local kind="$2"
    local name="$3"

    # Find the file containing the specified kind and name
    local file
    file=$(grep -rl "kind: $kind" "$dir_path" | xargs grep -l "name: $name")

    # Extract the apiGroup from the found file
    if [ -n "$file" ]; then
        local api_group
        api_group=$(yq eval '.apiVersion' "$file" | awk -F'/' '{print $1}')
        echo "$api_group"
    fi
}

# Function to get the generated clusterrole resource names
function generated_cluster_role_names() {
    local csvFile="${1}"
    local generated_cluster_role_names=()
    csv_name=$(yq eval -r '.metadata.name' "${csvFile}")
    cperms=$(yq eval -o=json -r '.spec.install.spec.clusterPermissions? // []' "$csvFile" | jq -c '.[] | {serviceAccountName, rules: [.rules[] | {verbs, apiGroups, resources, resourceNames, nonResourceURLs} | with_entries(select(.value != null and .value != []))]}')
    rbacPerms=$(yq eval -o=json -r '.spec.install.spec.permissions? // []' "$csvFile" | jq -c '.[] | {serviceAccountName, rules: [.rules[] | {verbs, apiGroups, resources, resourceNames, nonResourceURLs} | with_entries(select(.value != null and .value != []))]}')
    allPerms=("${cperms[@]}" "${rbacPerms[@]}")
    for perm in "${allPerms[@]}"; do
        sa=$(echo "$perm" | yq eval -r '.serviceAccountName')
        generated_name="$(generate_name "${csv_name}-${sa}" "${perm}")"
        generated_cluster_role_names+=("${generated_name}")
    done
    echo "${generated_cluster_role_names[@]}"
}

# Get CSV from manifest directory
function find_csv() {
    local manifest_dir="${1}"
    local csv_files

    # Use grep -l to find files containing "kind: ClusterServiceVersion"
    csv_files=$(grep -l "kind: ClusterServiceVersion" "$manifest_dir"/*.yaml)

    # Check if multiple CSV files are found
    if [ "$(echo "$csv_files" | wc -l)" -gt 1 ]; then
        echo "Error: Multiple CSV files found in ${manifest_dir}."
        return 1
    elif [ -z "$csv_files" ]; then
        echo "Error: No CSV file found in ${manifest_dir}."
        return 1
    else
        echo "${csv_files}"
    fi
}