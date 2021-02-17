#!/usr/bin/env bash

set -Eeuo pipefail

trap cleanup SIGINT SIGTERM ERR EXIT

# script_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")" &>/dev/null && pwd -P)

source ./dev-scripts/utils.sh
source ./dev-scripts/compile.sh
source ./dev-scripts/build.sh
source ./dev-scripts/deploy.sh

usage() {
  cat <<EOF
Usage: $(basename "${BASH_SOURCE[0]}") [-h]

Script description here.

Available options:

-h, --help      Print this help and exit
TODO
EOF
    exit
}

# script cleanup here
cleanup() {
    trap - SIGINT SIGTERM ERR EXIT
}


setup_colors

case $1 in
    compile | build | deploy)
        $1 "${@:2}"
    ;;
    help | usage | *)
        usage
    ;;
esac
