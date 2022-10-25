#! /bin/bash

set -o pipefail
set -o nounset
set -o errexit

FILE=packages.json

function registry_get_packages() {
    if [[ ! -f $FILE ]]; then
        grpcurl -plaintext  localhost:50051 api.Registry/ListPackages | jq '.name' > "$FILE"
    fi
}

function create_platform_operators() {
    local file=$1

    while IFS="" read -r p || [ -n "$p" ]
    do
cat <<EOF | kubectl apply -f -
---
apiVersion: platform.openshift.io/v1alpha1
kind: PlatformOperator
metadata:
name: $p
spec:
package:
    name: $p
EOF
done <"$file"
}

registry_get_packages
create_platform_operators "$FILE"
