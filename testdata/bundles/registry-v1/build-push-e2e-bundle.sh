#! /bin/bash

set -o errexit
set -o nounset
set -o pipefail

help="
build-push-e2e-bundle.sh is a script to build and push the e2e bundle image using kaniko.
Usage:
  build-push-e2e-bundle.sh [NAMESPACE] [TAG] [BUNDLE_NAME] [BUNDLE_DIR]

Argument Descriptions:
  - NAMESPACE is the namespace the kaniko Job should be created in
  - TAG is the full tag used to build and push the catalog image
"

if [[ "$#" -ne 4 ]]; then
  echo "Illegal number of arguments passed"
  echo "${help}"
  exit 1
fi


namespace=$1
tag=$2
bundle_name=$3
package_name=$4
bundle_dir="testdata/bundles/registry-v1/${package_name}"

echo "${namespace}" "${tag}"

kubectl create configmap -n "${namespace}" --from-file="${bundle_dir}/Dockerfile" operator-controller-e2e-${bundle_name}.root

tgz="${bundle_dir}/manifests.tgz"
tar czf "${tgz}" -C "${bundle_dir}/" manifests metadata
kubectl create configmap -n "${namespace}" --from-file="${tgz}" operator-controller-${bundle_name}.manifests
rm "${tgz}"

# Remove periods from bundle name due to pod name issues
job_name=${bundle_name//.}

kubectl apply -f - << EOF
apiVersion: batch/v1
kind: Job
metadata:
  name: "kaniko-${job_name}"
  namespace: "${namespace}"
spec:
  template:
    spec:
      initContainers:
        - name: copy-manifests
          image: busybox
          command: ['sh', '-c', 'cp /manifests-data/* /manifests']
          volumeMounts:
            - name: manifests
              mountPath: /manifests
            - name: manifests-data
              mountPath: /manifests-data
      containers:
      - name: kaniko
        image: gcr.io/kaniko-project/executor:latest
        args: ["--dockerfile=/workspace/Dockerfile",
                "--context=tar:///workspace/manifests/manifests.tgz",
                "--destination=${tag}",
                "--skip-tls-verify"]
        volumeMounts:
          - name: dockerfile
            mountPath: /workspace/
          - name: manifests
            mountPath: /workspace/manifests/
      restartPolicy: Never
      volumes:
        - name: dockerfile
          configMap:
            name: operator-controller-e2e-${bundle_name}.root
        - name: manifests
          emptyDir: {}
        - name: manifests-data
          configMap:
            name: operator-controller-${bundle_name}.manifests
EOF

kubectl wait --for=condition=Complete -n "${namespace}" jobs/kaniko-${job_name} --timeout=60s
