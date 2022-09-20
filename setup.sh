#!/usr/bin/env bash
set -euo pipefail
IFS=$'\n\t'

PLATFORM=${1:-"linux"}
ARCH=${2:-"amd64"}

DOCKER_VERSION="v20.10.9"
DOCKER_COMPOSE_VERSION="v2.10.2"
KUBECTL_VERSION="v1.24.1"

mkdir -p dist/

/usr/bin/env bash ./build/download_docker_binary.sh "$PLATFORM" "$ARCH" "$DOCKER_VERSION"
/usr/bin/env bash ./build/download_docker_compose_binary.sh "$PLATFORM" "$ARCH" "$DOCKER_COMPOSE_VERSION"
/usr/bin/env bash ./build/download_kubectl_binary.sh "$PLATFORM" "$ARCH" "$KUBECTL_VERSION"


