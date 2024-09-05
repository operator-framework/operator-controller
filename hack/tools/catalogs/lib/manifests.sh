# Library of functions for generating kube manifests

# Function to generate the target install namespace
function generate_namespace() {
    cat <<EOF
---
apiVersion: v1
kind: Namespace
metadata:
  name: \${NAMESPACE}
EOF
}

function generate_service_account() {
    cat <<EOF
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: \${SERVICE_ACCOUNT_NAME}
  namespace: \${NAMESPACE}
EOF
}

function generate_cluster_role() {
    cat <<EOF
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: \${CLUSTER_ROLE_NAME}
rules: $(echo "$CLUSTER_RBAC_RULES" | jq '.')
EOF
}

function generate_cluster_role_binding() {
    cat <<EOF
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: \${CLUSTER_ROLE_NAME}-binding
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: \${CLUSTER_ROLE_NAME}
subjects:
  - kind: ServiceAccount
    name: \${SERVICE_ACCOUNT_NAME}
    namespace: \${NAMESPACE}
EOF
}

function generate_role() {
    cat <<EOF
---
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: \${ROLE_NAME}
  namespace: \${NAMESPACE}
rules: $(echo "$NAMESPACE_RBAC_RULES" | jq '.')
EOF
}

function generate_role_binding() {
    cat << EOF
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: \${ROLE_NAME}-binding
  namespace: \${NAMESPACE}
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: \${ROLE_NAME}
subjects:
  - kind: ServiceAccount
    name: ${SERVICE_ACCOUNT_NAME}
    namespace: \${NAMESPACE}
EOF
}

function generate_cluster_extension() {
    cat <<EOF
---
apiVersion: olm.operatorframework.io/v1alpha1
kind: ClusterExtension
metadata:
  name: \${EXTENSION_NAME}
spec:
  source:
    sourceType: Catalog
    catalog:
      packageName: \${PACKAGE_NAME}
      version: \${PACKAGE_VERSION}
  install:
    namespace: \${NAMESPACE}
    serviceAccount:
      name: \${SERVICE_ACCOUNT_NAME}
EOF
}

function generate_rbac_manifests() {
    if [ "$(echo "$CLUSTER_RBAC_RULES" | jq length)" -gt 0 ]; then
        generate_cluster_role
        generate_cluster_role_binding
    fi
    if [ "$(echo "$NAMESPACE_RBAC_RULES" | jq length)" -gt 0 ]; then
        generate_role
        generate_role_binding
    fi
}

function generate_install_manifests() {
    generate_namespace
    generate_service_account
    generate_rbac_manifests
    generate_cluster_extension
}