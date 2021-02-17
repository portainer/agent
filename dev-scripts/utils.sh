#!/usr/bin/env bash

function setup_colors() {
    if [[ -t 2 ]] && [[ -z "${NO_COLOR-}" ]] && [[ "${TERM-}" != "dumb" ]]; then
        NOFORMAT='\033[0m' RED='\033[0;31m' GREEN='\033[0;32m' ORANGE='\033[0;33m' BLUE='\033[0;34m' PURPLE='\033[0;35m' CYAN='\033[0;36m' YELLOW='\033[1;33m'
    else
        NOFORMAT='' RED='' GREEN='' ORANGE='' BLUE='' PURPLE='' CYAN='' YELLOW=''
    fi
}

function msg() {
    echo >&2 -e "${1-}"
}

function die() {
    local msg=$1
    local code=${2-1} # default exit status 1
    msg "$msg"
    exit "$code"
}

function default_image_name() {
    GIT_COMMIT_HASH=$(git rev-parse --short HEAD)
    GIT_BRANCH_NAME=$(git rev-parse --abbrev-ref HEAD)
    ret_value="portainerci/agent:${GIT_BRANCH_NAME//\//-}-${GIT_COMMIT_HASH}"
}