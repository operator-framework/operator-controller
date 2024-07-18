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
image=$2
tag=${image##*:}

echo "${namespace}" "${image}"  "${tag}"

kubectl create configmap -n "${namespace}" --from-file=testdata/catalogs/test-catalog-${tag}.Dockerfile operator-controller-e2e-${tag}.dockerfile
kubectl create configmap -n "${namespace}" --from-file=testdata/catalogs/test-catalog-${tag} operator-controller-e2e-${tag}.build-contents

kubectl apply -f - << EOF
apiVersion: batch/v1
kind: Job
metadata:
  name: "kaniko-${tag}"
  namespace: "${namespace}"
spec:
  template:
    spec:
      containers:
      - name: kaniko-${tag}
        image: gcr.io/kaniko-project/executor:latest
        args: ["--dockerfile=/workspace/test-catalog-${tag}.Dockerfile",
                "--context=/workspace/",
                "--destination=${image}",
                "--skip-tls-verify"]
        volumeMounts:
          - name: dockerfile
            mountPath: /workspace/
          - name: build-contents
            mountPath: /workspace/test-catalog-${tag}/
      restartPolicy: Never
      volumes:
        - name: dockerfile
          configMap:
            name: operator-controller-e2e-${tag}.dockerfile
            items:
              - key: test-catalog-${tag}.Dockerfile
                path: test-catalog-${tag}.Dockerfile
        - name: build-contents
          configMap:
            name: operator-controller-e2e-${tag}.build-contents
EOF

kubectl wait --for=condition=Complete -n "${namespace}" jobs/kaniko-${tag} --timeout=60s
