#!/usr/bin/env bash

set -e

OUTPUT=internal/shared/util/tlsprofiles/mozilla_data.json
INPUT=https://ssl-config.mozilla.org/guidelines/latest.json

if ! curl -L -s -f "${INPUT}" -o "${OUTPUT}"; then
    echo "ERROR: Failed to download ${INPUT} (HTTP error or connection failure)" >&2
    exit 1
fi
