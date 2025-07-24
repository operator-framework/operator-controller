#!/bin/bash

HELM=$(command -v helm)
if [ -z "${HELM}" ]; then
    echo "helm command not found"
    exit 1
fi

WANT_VER_MAJOR=3
WANT_VER_MINOR=18

LONG_VER=$(${HELM} version | sed -E 's/.*Version:"([0-9]*\.[0-9]*\.[0-9]*)".*/\1/')

OLDIFS="${IFS}"
IFS='.' HELM_VER=(${LONG_VER})
IFS="${OLDIFS}"

if [ ${#HELM_VER[*]} -ne 3 ]; then
    echo "Invalid helm version: ${HELM_VER}"
    exit 1
fi

HELM_MAJOR=${HELM_VER[0]}
HELM_MINOR=${HELM_VER[1]}

if [ "${HELM_MAJOR}" -ne "${WANT_VER_MAJOR}" ]; then
    echo "Expecting helm version ${WANT_VER_MAJOR}.x, found ${LONG_VER}"
    exit 1
fi

if [ "${HELM_MINOR}" -lt "${WANT_VER_MINOR}" ]; then
    echo "Expecting helm minimum version ${WANT_VER_MAJOR}.${WANT_VER_MINOR}.x, found ${LONG_VER}"
    exit 1
fi
