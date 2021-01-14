#!/usr/bin/env bash

PLATFORM=${1:-"linux"}
ARCH=${2:-"x86_64"}

DOCKER_VERSION_LINUX="19.03.13"
DOCKER_VERSION_WINDOWS="19-03-12"

DOWNLOAD_FOLDER=".tmp/download"

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

exit 0
