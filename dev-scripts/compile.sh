#!/usr/bin/env bash

function compile() {
    parse_compile_params "${@:1}"

    local TARGET_DIST=dist/agent
    mkdir -p $TARGET_DIST

    msg "Compilation..."

    cd cmd/agent || exit 1
    GOOS="linux" GOARCH="amd64" CGO_ENABLED=0 go build -a --installsuffix cgo --ldflags '-s'
    rc=$?
    if [[ $rc != 0 ]]; then exit $rc; fi
    cd ../..
    mv cmd/agent/agent $TARGET_DIST

    msg "Agent executable is available on $TARGET_DIST"
}

function parse_compile_params() {
    while :; do
        case "${1-}" in
        -h | --help) usage_compile ;;
        -v | --verbose) set -x ;;
        -?*) die "Unknown option: $1" ;;
        *) break ;;
        esac
        shift
    done

    return 0
}

function usage_compile() {
    cmd="./dev.sh"
    cat <<EOF
Usage: $cmd compile [-h] [-v|--verbose]

This script is intended to help with compiling of the agent codebase

Available flags:
-h, --help              Print this help and exit
-v, --verbose           Verbose output
EOF
    exit
}
