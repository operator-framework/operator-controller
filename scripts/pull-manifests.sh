#!/bin/bash
set -euo pipefail
IFS=$'\n\t'
shopt -s extglob

catalogd_version=$CATALOGD_VERSION
cert_mgr_version=$CERT_MGR_VERSION
rukpak_version=$RUKPAK_VERSION
manifestsToClean=()

# Let's check if the manifests for cert-manager, rukpak, and catalogd are available in the local filesystem
# If they are, we'll use those instead of pulling from GitHub
if [[ -f "testdata/manifests/cert-manager-${cert_mgr_version}.yaml" ]]; then
    echo "cert-manager manifest is up-to-date"
else
    echo "Pulling cert-manager manifest from GitHub"
    curl -L "https://github.com/cert-manager/cert-manager/releases/download/${cert_mgr_version}/cert-manager.yaml" -o "testdata/manifests/cert-manager-${cert_mgr_version}.yaml"
    manifestsToClean+=( $(ls -d testdata/manifests/cert-manager-*!\(testdata/manifests/cert-manager-${cert_mgr_version}.yaml\)) ) || true
fi

if [[ -f "testdata/manifests/rukpak-${rukpak_version}.yaml" ]]; then
    echo "rukpak manifest is up-to-date"
else
    echo "Pulling rukpak manifest from GitHub"
    curl -L "https://github.com/operator-framework/rukpak/releases/download/${rukpak_version}/rukpak.yaml" -o "testdata/manifests/rukpak-${rukpak_version}.yaml"
    manifestsToClean+=( $(ls -d testdata/manifests/rukpak-*!\(testdata/manifests/rukpak-${rukpak_version}.yaml\)) ) || true
fi

if [[ -f "testdata/manifests/catalogd-${catalogd_version}.yaml" ]]; then
    echo "catalogd manifest is up-to-date"
else
    echo "Pulling catalogd manifest from GitHub"
    curl -L "https://github.com/operator-framework/catalogd/releases/download/${catalogd_version}/catalogd.yaml" -o "testdata/manifests/catalogd-${catalogd_version}.yaml"
    manifestsToClean+=( $(ls -d testdata/manifests/catalogd-*!\(catalogd-${catalogd_version}.yaml\)) ) || true
fi

for manifest in "${manifestsToClean[@]}"; do
    rm -f "${manifest}"
done
