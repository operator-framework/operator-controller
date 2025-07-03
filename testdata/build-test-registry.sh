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

kubectl wait --for=condition=Available -n "${namespace}" "deploy/${name}" --timeout=60s

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

kubectl wait --for=condition=Complete -n "${namespace}" "job/${name}-push" --timeout=60s
