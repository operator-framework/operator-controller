#! /bin/bash

set -o errexit
set -o nounset
set -o pipefail

help="
build-push-e2e-catalog.sh is a script to build and push the e2e catalog image using kaniko.
Usage:
  build-push-e2e-catalog.sh [NAMESPACE] [TAG]

Argument Descriptions:
  - NAMESPACE is the namespace the kaniko Job should be created in
  - TAG is the full tag used to build and push the catalog image
"

if [[ "$#" -ne 2 ]]; then
  echo "Illegal number of arguments passed"
  echo "${help}"
  exit 1
fi

namespace=$1
tag=$2

echo "${namespace}" "${tag}"

kubectl create configmap -n "${namespace}" --from-file=testdata/catalogs/test-catalog.Dockerfile operator-controller-e2e.dockerfile
kubectl create configmap -n "${namespace}" --from-file=testdata/catalogs/test-catalog operator-controller-e2e.build-contents

kubectl apply -f - << EOF
apiVersion: batch/v1
kind: Job
metadata:
  name: kaniko
  namespace: "${namespace}"
spec:
  template:
    spec:
      containers:
      - name: kaniko
        image: gcr.io/kaniko-project/executor:latest
        args: ["--dockerfile=/workspace/test-catalog.Dockerfile",
                "--context=/workspace/",
                "--destination=${tag}",
                "--skip-tls-verify"]
        volumeMounts:
          - name: dockerfile
            mountPath: /workspace/
          - name: build-contents
            mountPath: /workspace/test-catalog/
      restartPolicy: Never
      volumes:
        - name: dockerfile
          configMap:
            name: operator-controller-e2e.dockerfile
            items:
              - key: test-catalog.Dockerfile
                path: test-catalog.Dockerfile
        - name: build-contents
          configMap:
            name: operator-controller-e2e.build-contents
EOF

kubectl wait --for=condition=Complete -n "${namespace}" jobs/kaniko --timeout=60s
