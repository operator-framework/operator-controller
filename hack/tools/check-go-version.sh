#!/bin/bash

GO_VER=$1
OLDIFS="${IFS}"
IFS='.' INPUT_VERS=(${GO_VER})
IFS="${OLDIFS}"

if [ ${#INPUT_VERS[*]} -ne 3 -a ${#INPUT_VERS[*]} -ne 2 ]; then
    echo "Invalid go version: ${GO_VER}"
    exit 1
fi

GO_MAJOR=${INPUT_VERS[0]}
GO_MINOR=${INPUT_VERS[1]}
GO_PATCH=${INPUT_VERS[2]}

check_version () {
    whole=$1
    file=$2
    OLDIFS="${IFS}"
    IFS='.' ver=(${whole})
    IFS="${OLDIFS}"

    if [ ${#ver[*]} -eq 2 ] ; then
        if [ ${ver[0]} -gt ${GO_MAJOR} ] ; then
            echo "Bad golang version ${whole} in ${file} (expected ${GO_VER} or less)"
            exit 1
        fi
        if [ ${ver[1]} -gt ${GO_MINOR} ] ; then
            echo "Bad golang version ${whole} in ${file} (expected ${GO_VER} or less)"
            exit 1
        fi
        echo "Version ${whole} in ${file} is good"
        return
    fi
    if [ ${#INPUT_VERS[*]} -eq 2 ]; then
        echo "Bad golang version ${whole} in ${file} (expecting only major.minor version)"
        exit 1
    fi
    if [ ${#ver[*]} -ne 3 ] ; then
        echo "Badly formatted golang version ${whole} in ${file}"
        exit 1
    fi

    if [ ${ver[0]} -gt ${GO_MAJOR} ]; then
        echo "Bad golang version ${whole} in ${file} (expected ${GO_VER} or less)"
        exit 1
    fi
    if [ ${ver[1]} -gt ${GO_MINOR} ]; then
        echo "Bad golang version ${whole} in ${file} (expected ${GO_VER} or less)"
        exit 1
    fi
    if [ ${ver[1]} -eq ${GO_MINOR} -a ${ver[2]} -gt ${GO_PATCH} ]; then
        echo "Bad golang version ${whole} in ${file} (expected ${GO_VER} or less)"
        exit 1
    fi
    echo "Version ${whole} in ${file} is good"
}

for f in $(find . -name "*.mod"); do
    v=$(sed -En 's/^go (.*)$/\1/p' ${f})
    if [ -z ${v} ]; then
        echo "Skipping ${f}: no version found"
    else
        check_version ${v} ${f}
    fi
done
