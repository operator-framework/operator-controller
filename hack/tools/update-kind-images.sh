#!/bin/bash
set -euo pipefail
# Regenerates the KIND_IMAGES block in validate_kindest_node.sh
# by fetching released kindest/node images from the kind GitHub release.
#
# Usage: update-kind-images.sh <kind-binary>

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
VALIDATE_SCRIPT="${SCRIPT_DIR}/validate_kindest_node.sh"

KIND="${1:?Usage: $0 <kind-binary>}"

KIND_VER=$(${KIND} version | grep -oE 'v[0-9]+\.[0-9]+\.[0-9]+' || true)
if [ -z "${KIND_VER}" ]; then
    echo "Error: could not determine kind version." >&2
    exit 1
fi

echo "Fetching kindest/node images for kind ${KIND_VER}..."
RELEASE_BODY=$(curl -sfL "https://api.github.com/repos/kubernetes-sigs/kind/releases/tags/${KIND_VER}")
IMAGES=$(echo "${RELEASE_BODY}" | grep -oE 'kindest/node:v[0-9]+\.[0-9]+\.[0-9]+' | sort -u || true)
if [ -z "${IMAGES}" ]; then
    echo "Error: no kindest/node images found for kind ${KIND_VER}." >&2
    exit 1
fi

TMP=$(mktemp)
trap 'rm -f "${TMP}"' EXIT

awk -v ver="${KIND_VER}" -v images="${IMAGES}" \
  'BEGIN{in_block=0}
   /^# --- BEGIN KIND IMAGES ---/{print; in_block=1;
     print "# kind " ver
     print "case \"${K8S_MINOR}\" in"
     n=split(images,a,"\n")
     for(i=1;i<=n;i++){if(a[i]=="")continue
       minor=a[i]; sub(/.*:v/,"",minor); sub(/\.[0-9]+$/,"",minor)
       print "  " minor ") IMAGE=\"" a[i] "\" ;;"}
     print "  *) IMAGE=\"\" ;;"
     print "esac"; next}
   /^# --- END KIND IMAGES ---/{in_block=0; print; next}
   in_block{next}
   {print}' "${VALIDATE_SCRIPT}" > "${TMP}"

mv "${TMP}" "${VALIDATE_SCRIPT}"
chmod +x "${VALIDATE_SCRIPT}"
echo "Updated ${VALIDATE_SCRIPT} with images for kind ${KIND_VER}"
