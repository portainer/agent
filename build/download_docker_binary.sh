#!/usr/bin/env bash
download_docker_binary() {
    local PLATFORM=$1
    local ARCH=$2
    
    local DOCKER_VERSION=$3
    
    DOWNLOAD_FOLDER=".tmp/download"
    
    if [ "$PLATFORM" = "windows" ]; then
        PLATFORM="win"
    fi
    
    if [ "$ARCH" = "amd64" ]; then
        ARCH="x86_64"
    fi

    if [ "$ARCH" = "arm" ]; then
        ARCH="armhf"
    fi

    if [ "$ARCH" = "arm64" ]; then
        ARCH="aarch64"
    fi

    
    rm -rf "${DOWNLOAD_FOLDER}"
    mkdir -pv "${DOWNLOAD_FOLDER}"
    
    echo "Downloading docker binaries for ${PLATFORM} ${ARCH}"
    
    if [ "${PLATFORM}" == 'win' ]; then
        wget -O "${DOWNLOAD_FOLDER}/docker-binaries.zip" "https://dockermsft.azureedge.net/dockercontainer/docker-${DOCKER_VERSION}.zip"
        unzip "${DOWNLOAD_FOLDER}/docker-binaries.zip" -d "${DOWNLOAD_FOLDER}"
        mv "${DOWNLOAD_FOLDER}/docker/docker.exe" dist/
        mv ${DOWNLOAD_FOLDER}/docker/*.dll dist/
    else
        wget -O "${DOWNLOAD_FOLDER}/docker-binaries.tgz" "https://download.docker.com/${PLATFORM}/static/stable/${ARCH}/docker-${DOCKER_VERSION}.tgz"
        tar -xf "${DOWNLOAD_FOLDER}/docker-binaries.tgz" -C "${DOWNLOAD_FOLDER}"
        mv "${DOWNLOAD_FOLDER}/docker/docker" dist/
    fi
    
    echo "Docker binary download to dist/"
}