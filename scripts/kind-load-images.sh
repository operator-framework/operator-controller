#!/bin/bash
set -euo pipefail
IFS=$'\n\t'

containerImages=( $(cat testdata/manifests/*.yaml | sed -n 's/.*image: \(.*\)/\1/p' |  sed 's/\"//g') )
for image in "${containerImages[@]}"; do
    docker pull "${image}"
    kind load docker-image "${image}" --name operator-controller-e2e
done
