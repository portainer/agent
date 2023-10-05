#!/usr/bin/env bash
set -euo pipefail
IFS=$'\n\t'

PLATFORM=${1:-"linux"}
ARCH=${2:-"amd64"}

BINARY_VERSION_FILE="./binary-version.json"

dockerVersion=$(jq -r '.docker' < "${BINARY_VERSION_FILE}")
dockerComposeVersion=$(jq -r '.dockerCompose' < "${BINARY_VERSION_FILE}")
kubectlVersion=$(jq -r '.kubectl' < "${BINARY_VERSION_FILE}")
mingitVersion=$(jq -r '.mingit' < "${BINARY_VERSION_FILE}")

echo "Downloading binaries for docker ${dockerVersion}, docker-compose ${dockerComposeVersion}, kubectl ${kubectlVersion}, and mingit ${mingitVersion}"

mkdir -p dist/

/usr/bin/env bash ./build/download_docker_binary.sh "$PLATFORM" "$ARCH" "$dockerVersion"
/usr/bin/env bash ./build/download_docker_compose_binary.sh "$PLATFORM" "$ARCH" "$dockerComposeVersion"
/usr/bin/env bash ./build/download_kubectl_binary.sh "$PLATFORM" "$ARCH" "$kubectlVersion"
/usr/bin/env bash ./build/download_mingit_binary.sh "$PLATFORM" "$ARCH" "$mingitVersion"
