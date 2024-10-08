# Library of functions for unpacking bundles

create_container() {
    local image="${1}"
    # Create a container from the image
    # Catalog images don't have an executable, but some entrypoint is required (though it will be ignored)
    local container_id
    echo "Creating container for bundle image (${image}). This may take a few seconds if the image needs to be pulled..." >&2
    container_id=$($CONTAINER_RUNTIME create --quiet --entrypoint="/bin/bash" "$image")
    if [ -z "$container_id" ]; then
        echo "Failed to create container from image '$image'." >&2
        exit 1
    fi

    echo "${container_id}"
}

unpack() {
    local container_id="${1}"
    local output_dir="${2}"

    # Extract the directory from the "operators.operatorframework.io.bundle.manifests.v1" label
    local manifest_dir
    echo "Unpacking bundle to ${output_dir}" >&2
    manifest_dir=$($CONTAINER_RUNTIME inspect --format '{{ index .Config.Labels "operators.operatorframework.io.bundle.manifests.v1" }}' "$container_id")

    if [ -z "$manifest_dir" ]; then
        echo "No manifest directory label found on the image."
        $CONTAINER_RUNTIME rm "$container_id"
        exit 1
    fi

    # Copy files from the container to a temporary directory
    $CONTAINER_RUNTIME cp "$container_id:$manifest_dir/." "$output_dir" > /dev/null

    # Clean up the container
    $CONTAINER_RUNTIME rm "$container_id" > /dev/null
}