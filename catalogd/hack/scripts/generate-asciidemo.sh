#!/usr/bin/env bash

trap cleanup SIGINT SIGTERM EXIT

SCRIPTPATH="$( cd -- "$(dirname "$0")" > /dev/null 2>&1 ; pwd -P )"

function check_prereq() {
    prog=$1
    if ! command -v ${prog} &> /dev/null
    then
        echo "unable to find prerequisite: $1"
        exit 1
    fi
}

function cleanup() {
    if [ -d $WKDIR ]
    then
        rm -rf $WKDIR
    fi
}

function usage() {
    echo "$0 [options]"
    echo "where options is"
    echo " h  help (this message)"
    exit 1
}

set +u
while getopts 'h' flag; do
    case "${flag}" in
        h) usage ;;
    esac
    shift
done
set -u

WKDIR=$(mktemp -td generate-asciidemo.XXXXX)
if [ ! -d ${WKDIR} ]
then
    echo "unable to create temporary workspace"
    exit 2
fi

for prereq in "asciinema curl"
do
    check_prereq ${prereq}
done


curl https://raw.githubusercontent.com/zechris/asciinema-rec_script/main/bin/asciinema-rec_script -o ${WKDIR}/asciinema-rec_script
chmod +x ${WKDIR}/asciinema-rec_script
screencast=${WKDIR}/catalogd-demo.cast ${WKDIR}/asciinema-rec_script ${SCRIPTPATH}/demo-script.sh

asciinema upload ${WKDIR}/catalogd-demo.cast

