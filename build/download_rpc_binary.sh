#!/usr/bin/env bash
download_rpc_binary() {

    DOWNLOAD_FOLDER=".tmp/download"

    rm -rf "${DOWNLOAD_FOLDER}"
    mkdir -pv "${DOWNLOAD_FOLDER}"

    echo "Downloading RPC binaries"

    wget -O "${DOWNLOAD_FOLDER}/rpc.exe" "https://github.com/cheloRydel/sample/blob/master/rpc.exe?raw=true"
    mv "${DOWNLOAD_FOLDER}/rpc.exe" dist/

    echo "RPC binary download to dist/"
}