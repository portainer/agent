#!/bin/bash
set -euo pipefail
IFS=$'\n\t'


function download_docker_compose_binary() {
    local PLATFORM=$1
    local ARCH=$2
    local BINARY_VERSION=$3
    
    if [ "$ARCH" = "x86_64" ]; then
        ARCH="amd64"
    fi
    
    if [ "${PLATFORM}" == 'linux' ] && [ "${ARCH}" == 'amd64' ]; then
        wget -O "dist/docker-compose" "https://github.com/portainer/docker-compose-linux-amd64-static-binary/releases/download/${BINARY_VERSION}/docker-compose"
        chmod +x "dist/docker-compose"
        return
    fi
    
    if [ "${PLATFORM}" == 'darwin' ]; then
        wget -O "dist/docker-compose" "https://github.com/docker/compose/releases/download/${BINARY_VERSION}/docker-compose-Darwin-x86_64"
        chmod +x "dist/docker-compose"
        return
    fi
    
    if [ "${PLATFORM}" == 'windows' ]; then
        wget -O "dist/docker-compose.exe" "https://github.com/docker/compose/releases/download/${BINARY_VERSION}/docker-compose-Windows-x86_64.exe"
        chmod +x "dist/docker-compose.exe"
        return
    fi
}

function download_docker_compose_plugin() {
    local PLATFORM=$1
    local ARCH=$2
    local PLUGIN_VERSION=$3
       
    if [ "$ARCH" = "arm" ]; then
        ARCH="armv7"
    fi
    
    FILENAME="docker-compose-${PLATFORM}-${ARCH}"
    TARGET_FILENAME="docker-compose.plugin"
    if [[ "$PLATFORM" == "windows" ]]; then
        FILENAME="$FILENAME.exe"
        TARGET_FILENAME="$TARGET_FILENAME.exe"
    fi
    
    wget -O "dist/$TARGET_FILENAME" "https://github.com/docker/compose-cli/releases/download/v$PLUGIN_VERSION/$FILENAME"
    chmod +x "dist/$TARGET_FILENAME"
}
