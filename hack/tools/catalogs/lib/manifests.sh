# Library of functions for generating kube manifests

# Function to generate the target install namespace
generate_namespace() {
    cat <<EOF
---
apiVersion: v1
kind: Namespace
metadata:
  name: \${NAMESPACE}
EOF
}

generate_cluster_extension() {
    cat <<EOF
---
apiVersion: olm.operatorframework.io/v1
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
EOF
}

generate_install_manifests() {
    generate_namespace
    generate_cluster_extension
}
