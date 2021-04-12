#!/usr/bin/env bash
#!/bin/bash
set -euo pipefail
IFS=$'\n\t'

PLATFORM=${1:-"linux"}
ARCH=${2:-"x86_64"}

DOCKER_VERSION_LINUX="19.03.13"
DOCKER_VERSION_WINDOWS="19-03-12"
KUBECTL_VERSION="v1.18.0"

source ./build/linux/download_docker_binary.sh
source ./build/linux/download_kubectl_binary.sh

download_docker_binary $PLATFORM $ARCH $DOCKER_VERSION_LINUX $DOCKER_VERSION_WINDOWS
download_kubectl_binary $PLATFORM $ARCH $KUBECTL_VERSION