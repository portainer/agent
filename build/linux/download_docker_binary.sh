#!/usr/bin/env bash
download_docker_binary() {
  local PLATFORM=${1:-"linux"}
  local ARCH=${2:-"x86_64"}

  DOCKER_VERSION_LINUX=$3
  DOCKER_VERSION_WINDOWS=$4

  DOWNLOAD_FOLDER=".tmp/download"

  if [ "$PLATFORM" = "windows" ]; then
    PLATFORM="win"
  fi

  if [ "$ARCH" = "amd64" ]; then
    ARCH="x86_64"
  fi

  rm -rf "${DOWNLOAD_FOLDER}"
  mkdir -pv "${DOWNLOAD_FOLDER}"

  echo "Downloading docker binaries for ${PLATFORM} ${ARCH}"

  if [ "${PLATFORM}" == 'win' ]; then
    wget -O "${DOWNLOAD_FOLDER}/docker-binaries.zip" "https://dockermsft.azureedge.net/dockercontainer/docker-${DOCKER_VERSION_WINDOWS}.zip"
    unzip "${DOWNLOAD_FOLDER}/docker-binaries.zip" -d "${DOWNLOAD_FOLDER}"
    mv "${DOWNLOAD_FOLDER}/docker/docker.exe" dist/
  else
    wget -O "${DOWNLOAD_FOLDER}/docker-binaries.tgz" "https://download.docker.com/${PLATFORM}/static/stable/${ARCH}/docker-${DOCKER_VERSION_LINUX}.tgz"
    tar -xf "${DOWNLOAD_FOLDER}/docker-binaries.tgz" -C "${DOWNLOAD_FOLDER}"
    mv "${DOWNLOAD_FOLDER}/docker/docker" dist/
  fi
}