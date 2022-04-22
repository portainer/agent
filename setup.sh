#!/usr/bin/env bash
#!/bin/bash
set -euo pipefail
IFS=$'\n\t'

PLATFORM=${1:-"linux"}
ARCH=${2:-"amd64"}

DOCKER_VERSION_LINUX="19.03.13"
DOCKER_VERSION_WINDOWS="19-03-12"

DOCKER_COMPOSE_VERSION_LINUX="1.27.4"
DOCKER_COMPOSE_VERSION_WINDOWS="1.28.0"
DOCKER_COMPOSE_PLUGIN_VERSION="2.0.0-rc.2"
KUBECTL_VERSION="v1.18.0"

DOCKER_VERSION=$DOCKER_VERSION_LINUX
DOCKER_COMPOSE_VERSION=$DOCKER_COMPOSE_VERSION_LINUX

mkdir -p dist/

if [[ "$PLATFORM" == "windows" ]];
then
    DOCKER_VERSION=$DOCKER_VERSION_WINDOWS
    DOCKER_COMPOSE_VERSION=$DOCKER_COMPOSE_VERSION_WINDOWS
fi



source ./build/download_docker_binary.sh
source ./build/download_kubectl_binary.sh
source ./build/download_docker_compose_binary.sh

download_docker_binary "$PLATFORM" "$ARCH" "$DOCKER_VERSION"
download_kubectl_binary "$PLATFORM" "$ARCH" "$KUBECTL_VERSION"

if [ "$PLATFORM" == "linux" ] && [ "$ARCH" != "amd64" ] && [ "$ARCH" != "x86_64" ]; then
    download_docker_compose_plugin "$PLATFORM" "$ARCH" "$DOCKER_COMPOSE_PLUGIN_VERSION"
else
    download_docker_compose_binary "$PLATFORM" "$ARCH" "$DOCKER_COMPOSE_VERSION"
fi

