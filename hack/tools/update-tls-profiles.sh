#!/usr/bin/env bash

set -e

OUTPUT=internal/shared/util/tlsprofiles/mozilla_data.json
INPUT=https://ssl-config.mozilla.org/guidelines/latest.json
tmp="$(mktemp "${OUTPUT}.tmp.XXXXXX")"
trap 'rm -f "${tmp}"' EXIT

if ! curl -L -s -f "${INPUT}" -o "${tmp}"; then
    echo "ERROR: Failed to download ${INPUT} (HTTP error or connection failure)" >&2
    exit 1
fi

mv "${tmp}" "${OUTPUT}"
trap - EXIT
