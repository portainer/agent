#!/usr/bin/env bash

compile=0
image_name=""

function build_command() {
    parse_build_params "${@:1}"

    if [[ "$compile" == "1" ]]; then
        compile_command
    fi

    build "$image_name"
}

function build() {
    docker rmi -f "$1" &>/dev/null || true

    msg "Image build..."
    docker build --no-cache -t "$1" -f build/linux/Dockerfile . &>/dev/null

    msg "Image $1 is built"
}

function build_podman() {
    podman rmi -f "$1" &>/dev/null || true

    msg "Image build..."
    podman build --no-cache -t "$1" -f build/linux/Dockerfile . &>/dev/null

    msg "Image $1 is built"
}

function parse_build_params() {
    while :; do
        case "${1-}" in
        -h | --help) usage_build ;;
        -v | --verbose) set -x ;;
        -c | --compile) compile=1 ;;
        --image-name)
            image_name=$2
            shift
            ;;
        -?*) die "Unknown option: $1" ;;
        *) break ;;
        esac
        shift
    done

    if [[ "$image_name" == "" ]]; then
        local ret_value=""
        default_image_name
        image_name=$ret_value
    fi

    return 0
}

function usage_build() {
    cmd="./dev.sh"
    cat <<EOF
Usage: $cmd build [-h] [-v|--verbose] [-c|--compile] [--image-name IMAGE_NAME]

This script is intended to help with building docker imagesa

Available flags:
-h, --help                  Print this help and exit
-v, --verbose               Verbose output
-c, --compile               Compile the code before build
--image-name IMAGE_NAME     Choose a different image name (default: portainercy/agent:GIT_BRANCH:GIT_HASH)
EOF
    exit
}
