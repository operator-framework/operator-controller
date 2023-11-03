#! /bin/bash

set -o errexit
set -o nounset
set -o pipefail

help="
image-registry.sh is a script to stand up an image registry within a cluster.
Usage:
  image-registry.sh [NAMESPACE] [NAME]

Argument Descriptions:
  - NAMESPACE is the namespace that should be created and is the namespace in which the image registry will be created
  - NAME is the name that should be used for the image registry Deployment and Service
"

if [[ "$#" -ne 2 ]]; then
  echo "Illegal number of arguments passed"
  echo "${help}"
  exit 1
fi

namespace=$1
name=$2

kubectl apply -f - << EOF
apiVersion: v1
kind: Namespace
metadata:
  name: ${namespace}
---
apiVersion: cert-manager.io/v1
kind: Issuer
metadata:
  name: selfsigned-issuer
  namespace: ${namespace}
spec:
  selfSigned: {}
---
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: ${namespace}-registry
  namespace: ${namespace}
spec:
  secretName: ${namespace}-registry
  isCA: true
  dnsNames:
    - ${name}.${namespace}.svc
  privateKey:
    algorithm: ECDSA
    size: 256
  issuerRef:
    name: selfsigned-issuer
    kind: Issuer
    group: cert-manager.io
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: ${name}
  namespace: ${namespace}
  labels:
    app: registry
spec:
  replicas: 1
  selector:
    matchLabels:
      app: registry
  template:
    metadata:
      labels:
        app: registry
    spec:
      containers:
      - name: registry
        image: registry:2
        volumeMounts:
        - name: certs-vol
          mountPath: "/certs"
        env:
        - name: REGISTRY_HTTP_TLS_CERTIFICATE
          value: "/certs/tls.crt"
        - name: REGISTRY_HTTP_TLS_KEY
          value: "/certs/tls.key"
      volumes:
        - name: certs-vol
          secret:
            secretName: ${namespace}-registry
---
apiVersion: v1
kind: Service
metadata:
  name: ${name}
  namespace: ${namespace}
spec:
  selector:
    app: registry
  ports:
  - port: 5000
    targetPort: 5000
EOF

kubectl wait --for=condition=Available -n "${namespace}" "deploy/${name}" --timeout=60s
