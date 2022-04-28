#!/usr/bin/env bash
download_kubectl_binary(){
  local PLATFORM=$1
  local ARCH=$2
  local KUBECTL_VERSION=$3
  local KUBECTL_BIN_NAME="kubectl"

  if [ "$PLATFORM" = "windows" ]; then
    KUBECTL_BIN_NAME="kubectl.exe"
  fi

  wget -O "dist/$KUBECTL_BIN_NAME" "https://storage.googleapis.com/kubernetes-release/release/${KUBECTL_VERSION}/bin/${PLATFORM}/${ARCH}/${KUBECTL_BIN_NAME}"
  chmod +x "dist/$KUBECTL_BIN_NAME"
}