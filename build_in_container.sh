#!/usr/bin/env sh

platform=$1
arch=$2
binary="agent-$platform-$arch"

mkdir -p dist
rm dist/$binary
rm dist/agent

docker run --rm -tv "$(pwd):/src" -e BUILD_GOOS="$platform" -e BUILD_GOARCH="$arch" portainer/golang-builder:cross-platform /src/cmd/agent

mv "cmd/agent/$binary" dist/agent
