#!/usr/bin/env bash

ARCHIVE_BUILD_FOLDER="/tmp/portainer-builds"

if [[ $# -ne 1 ]] ; then
  echo "Usage: $(basename $0) <VERSION>"
  exit 1
fi

VERSION=$1

function clean() {
  rm -rf dist/*
}

function build_and_push_image() {
  docker build -t "portainer/agent:${1}-${VERSION}" .
  docker tag "portainer/agent:${1}-${VERSION}" "portainer/agent:${1}"
  docker push "portainer/agent:${1}-${VERSION}"
  docker push "portainer/agent:${1}"
}

function build_archive() {
  BUILD_FOLDER="${ARCHIVE_BUILD_FOLDER}/$1"
  rm -rf ${BUILD_FOLDER} && mkdir -pv ${BUILD_FOLDER}/agent
  mv dist/* ${BUILD_FOLDER}/agent/
  cd ${BUILD_FOLDER}
  tar cvpfz "portainer-agent-${VERSION}-$1.tar.gz" agent
  mv "portainer-agent-${VERSION}-$1.tar.gz" ${ARCHIVE_BUILD_FOLDER}/
  cd -
}

function build_binary() {
  name="agent"; if [ "$(echo "$tag" | cut -c1)"  = "w" ]; then name="${name}.exe"; fi
  docker run --rm -tv $(pwd):/src -e BUILD_GOOS="$1" -e BUILD_GOARCH="$2" portainer/golang-builder:cross-platform /src/cmd/agent
  mv "cmd/agent/agent-${1}-${2}" dist/${name}
}

function build_all() {
  for tag in $@; do
    os=`echo ${tag} | cut -d \- -f 1`
    arch=`echo ${tag} | cut -d \- -f 2`

    build_binary "${os}" "${arch}"
    if [ `echo $tag | cut -d \- -f 1` == 'linux' ]
    then
      build_and_push_image "${tag}"
    else
      build_archive "$tag"
    fi
    clean
  done
}

mkdir dist
build_all 'linux-amd64 linux-arm linux-arm64 linux-ppc64le linux-s390x windows-amd64'

exit 0
