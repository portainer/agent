#!/usr/bin/env bash

function clean() {
  rm -rf dist/*
}

function build_and_push_image() {
  tag=$1
  docker build -t "portainer/agent:${tag}" .
  docker push "portainer/agent:${tag}"
}

function build_binary() {
  os=$1
  arch=$2
  docker run --rm -tv $(pwd):/src -e BUILD_GOOS="$os" -e BUILD_GOARCH="$arch" portainer/golang-builder:cross-platform /src/cmd/agent
  mv "cmd/agent/agent-${os}-${arch}" dist/agent
}

function build_n_push() {
  os=$1
  arch=$2
  tag=$3
  clean
  build_binary "${os}" "${arch}"
  build_and_push_image "${tag}"
}

build_n_push "linux" "amd64" "develop"

exit 0
