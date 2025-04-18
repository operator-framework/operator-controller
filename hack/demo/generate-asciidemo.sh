#!/usr/bin/env bash

set -x

trap cleanup SIGINT SIGTERM EXIT

SCRIPTPATH="$( cd -- "$(dirname "$0")" > /dev/null 2>&1 ; pwd -P )"
export DEMO_RESOURCE_DIR="${SCRIPTPATH}/resources"

check_prereq() {
    prog=$1
    if ! command -v ${prog} &> /dev/null
    then
        echo "unable to find prerequisite: $1"
        exit 1
    fi
}

cleanup() {
    if [[ -n "${WKDIR-}" && -d $WKDIR ]]; then
        rm -rf $WKDIR
    fi
}

usage() {
    echo "$0 [options] <demo-script>"
    echo ""
    echo "options:"
    echo "  -n  <name>"
    echo "  -u  upload cast (default: false)"
    echo "  -h  help (this message)"
    echo ""
    echo "examples:"
    echo "  # Generate asciinema demo described by gzip-demo-script.sh into gzip-demo-script.cast"
    echo "  $0 gzip-demo-script.sh"
    echo ""
    echo "  # Generate asciinema demo described by demo-script.sh into catalogd-demo.cast"
    echo "  $0 -n catalogd-demo demo-script.sh"
    echo ""
    echo "  # Generate and upload catalogd-demo.cast"
    echo "  $0 -u -n catalogd-demo demo-script.sh"
    exit 1
}

set +u
while getopts ':hn:u' flag; do
    case "${flag}" in
        h)
            usage
            ;;
        n)
            DEMO_NAME="${OPTARG}"
            ;;
        u)
            UPLOAD=true
            ;;
        :)
            echo "Error: Option -${OPTARG} requires an argument."
            usage
            ;;
        \?)
            echo "Error: Invalid option -${OPTARG}"
            usage
            ;;
    esac
done
shift $((OPTIND - 1))
set -u

DEMO_SCRIPT="${1-}"

if [ -z $DEMO_SCRIPT ]; then
  usage
fi

WKDIR=$(mktemp -d -t generate-asciidemo.XXXXX)
if [ ! -d ${WKDIR} ]; then
    echo "unable to create temporary workspace"
    exit 2
fi

for prereq in "asciinema curl"; do
    check_prereq ${prereq}
done

curl https://raw.githubusercontent.com/zechris/asciinema-rec_script/main/bin/asciinema-rec_script -o ${WKDIR}/asciinema-rec_script
chmod +x ${WKDIR}/asciinema-rec_script
screencast=${WKDIR}/${DEMO_NAME}.cast ${WKDIR}/asciinema-rec_script ${SCRIPTPATH}/${DEMO_SCRIPT}

if [ -n "${UPLOAD-}" ]; then
  asciinema upload ${WKDIR}/${DEMO_NAME}.cast
fi

