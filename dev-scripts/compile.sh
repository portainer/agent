#!/usr/bin/env bash

AGENT_VERSION=${AGENT_VERSION:-""}

function compile_command() {
   parse_compile_params "${@:1}"

   compile
}

function compile() {
    compile_agent
    compile_credential_helper
}

function compile_agent() {
    msg "Compiling agent..."

    local TARGET_DIST=dist
    mkdir -p $TARGET_DIST

    cd cmd/agent || exit 1

    local cmd=(go build -trimpath --installsuffix cgo)

    ldflags="-s"
    if [[ -n "$AGENT_VERSION" ]]; then
        ldflags="$ldflags -X 'github.com/portainer/agent.Version=${AGENT_VERSION}'"
    fi

    GOOS="linux" GOARCH="$(go env GOARCH)" CGO_ENABLED=0 "${cmd[@]}" --ldflags "$ldflags"

    rc=$?
    if [[ $rc != 0 ]]; then exit $rc; fi
    cd ../..
    mv cmd/agent/agent $TARGET_DIST

    msg "Agent executable is available on $TARGET_DIST/agent"
}

function compile_credential_helper() {
    msg "Compiling credential helper"

    local TARGET_DIST=dist
    mkdir -p $TARGET_DIST

    cd cmd/docker-credential-portainer || exit 1
    GOOS="linux" GOARCH="$(go env GOARCH)" CGO_ENABLED=0 go build -trimpath --installsuffix cgo --ldflags '-s'
    rc=$?
    if [[ $rc != 0 ]]; then exit $rc; fi
    cd ../..
    mv cmd/docker-credential-portainer/docker-credential-portainer $TARGET_DIST

    msg "Credential helper executable is available on $TARGET_DIST/docker-credential-portainer"
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
    local cmd_name="./dev.sh"
    cat <<EOF
Usage: $cmd_name compile [-h] [-v|--verbose]

This script is intended to help with compiling of the agent codebase

Available flags:
-h, --help              Print this help and exit
-v, --verbose           Verbose output
EOF
    exit
}
