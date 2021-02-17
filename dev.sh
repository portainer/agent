#!/usr/bin/env bash

set -Eeuo pipefail

trap cleanup SIGINT SIGTERM ERR EXIT

script_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")" &>/dev/null && pwd -P)

source ./dev-scripts/utils.sh
source ./dev-scripts/compile.sh
source ./dev-scripts/build.sh
source ./dev-scripts/deploy.sh

usage() {
  cat <<EOF
Usage: $(basename "${BASH_SOURCE[0]}") [-h] [-v] [-f] -p param_value arg1 [arg2...]

Script description here.

Available options:

-h, --help      Print this help and exit
-v, --verbose   Print script debug info
-f, --flag      Some flag description
-p, --param     Some param description
EOF
  exit
}

cleanup() {
  trap - SIGINT SIGTERM ERR EXIT
  # script cleanup here
}


# parse_params "$@"
# cmd=$1
# parse_params ${@:1}
setup_colors

# script logic here

# msg "${RED}Read parameters:${NOFORMAT}"
# msg "cmd: ${cmd}"
# msg "- flag: ${flag}"
# msg "- param: ${param}"
# msg "- arguments: ${args[*]-}"

case $1 in
compile | build | deploy)
    $1 ${@:1}
  ;;
help | usage | *)
  usage
  ;;
esac
