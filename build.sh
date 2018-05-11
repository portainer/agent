#!/usr/bin/env bash

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

function build_binary() {
  docker run --rm -tv $(pwd):/src -e BUILD_GOOS="$1" -e BUILD_GOARCH="$2" portainer/golang-builder:cross-platform /src/cmd/agent
  mv "cmd/agent/agent-${1}-${2}" dist/agent
}

function build_all() {
  for tag in $@; do
    os=`echo ${tag} | cut -d \- -f 1`
    arch=`echo ${tag} | cut -d \- -f 2`

    build_binary "${os}" "${arch}"
    build_and_push_image "${tag}"
    clean
  done
}

build_all 'linux-amd64 linux-arm linux-arm64 linux-ppc64le linux-s390x'

exit 0
