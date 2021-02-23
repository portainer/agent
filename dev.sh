#!/usr/bin/env bash

set -Eeuo pipefail

trap cleanup SIGINT SIGTERM ERR EXIT

# script_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")" &>/dev/null && pwd -P)

source ./dev-scripts/utils.sh
source ./dev-scripts/compile.sh
source ./dev-scripts/build.sh
source ./dev-scripts/deploy.sh

usage() {
    cmd=$(basename "${BASH_SOURCE[0]}")
    cat <<EOF
Usage: $cmd command

This script is intended to help with compiling and deploying of dev enviroment

Available commands:

help      Print this help and exit
compile   Compile the codebase
build     Build a docker image
deploy    Deploy the agent image

To get help with a command use: $cmd command -h

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
    "$1"_command "${@:2}"
    ;;
help | usage)
    usage
    ;;
local)
    deploy_command --local -c "${@:2}"
    ;;
*)
    deploy_command -s --ip 10.0.7.10 --ip 10.0.7.11 -c "${@:2}"
    ;;
esac
