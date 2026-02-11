#!/bin/bash

set -o errexit
set -o nounset
set -o pipefail

help="
build-test-registry.sh is a script to stand up an image registry within a cluster.
Usage:
  build-test-registry.sh [NAMESPACE] [NAME] [IMAGE]

Argument Descriptions:
  - NAMESPACE is the namespace that should be created and is the namespace in which the image registry will be created
  - NAME is the name that should be used for the image registry Deployment and Service
  - IMAGE is the name of the image that should be used to run the image registry
"

if [[ "$#" -ne 3 ]]; then
  echo "Illegal number of arguments passed"
  echo "${help}"
  exit 1
fi

namespace=$1
name=$2
image=$3

kubectl apply -f - << EOF
apiVersion: v1
kind: Namespace
metadata:
  name: ${namespace}
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
    - ${name}.${namespace}.svc.cluster.local
    - ${name}-controller-manager-metrics-service.${namespace}.svc
    - ${name}-controller-manager-metrics-service.${namespace}.svc.cluster.local
  privateKey:
    rotationPolicy: Always
    algorithm: ECDSA
    size: 256
  issuerRef:
    name: olmv1-ca
    kind: ClusterIssuer
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
        image: registry:3
        imagePullPolicy: IfNotPresent
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
  - name: http
    port: 5000
    targetPort: 5000
    nodePort: 30000
  type: NodePort
EOF

kubectl wait --for=condition=Available -n "${namespace}" "deploy/${name}" --timeout=3m

kubectl apply -f - << EOF
apiVersion: batch/v1
kind: Job
metadata:
  name: ${name}-push
  namespace: "${namespace}"
spec:
  template:
    spec:
      restartPolicy: Never
      containers:
      - name: push
        image: ${image}
        command:
        - /push
        args: 
        - "--registry-address=${name}.${namespace}.svc:5000"
        - "--images-path=/images"
        volumeMounts:
        - name: certs-vol
          mountPath: "/certs"
        env:
        - name: SSL_CERT_DIR
          value: "/certs/"
      volumes:
        - name: certs-vol
          secret:
            secretName: ${namespace}-registry
EOF

kubectl wait --for=condition=Complete -n "${namespace}" "job/${name}-push" --timeout=3m
