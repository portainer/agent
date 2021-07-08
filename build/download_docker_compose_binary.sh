#!/usr/bin/env bash
function download_docker_compose_binary(){
    local PLATFORM=$1
    local ARCH=$2
    local DOCKER_COMPOSE_VERSION=$3
    
    echo "Downloading docker-compose binary for ${PLATFORM} ${ARCH}"
    
    
    if [ "${PLATFORM}" == 'linux' ] && [[ ("${ARCH}" == 'amd64') || ("${ARCH}" == 'x86_64')  ]]; then
        wget -O "dist/docker-compose" "https://github.com/portainer/docker-compose-linux-amd64-static-binary/releases/download/${DOCKER_COMPOSE_VERSION}/docker-compose"
        chmod +x "dist/docker-compose"
        elif [ "${PLATFORM}" == 'mac' ]; then
        wget -O "dist/docker-compose" "https://github.com/docker/compose/releases/download/${DOCKER_COMPOSE_VERSION}/docker-compose-Darwin-x86_64"
        chmod +x "dist/docker-compose"
        elif [ "${PLATFORM}" == 'win' ]; then
        wget -O "dist/docker-compose.exe" "https://github.com/docker/compose/releases/download/${DOCKER_COMPOSE_VERSION}/docker-compose-Windows-x86_64.exe"
        chmod +x "dist/docker-compose.exe"
    fi
    
    echo "Docker-compose binary download to dist/"
    
}