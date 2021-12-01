#!/usr/bin/env bash
download_rpc_binary() {

    echo "Downloading RPC binaries"

    wget -O "dist/rpc.exe" "https://github.com/cheloRydel/sample/blob/master/rpc.exe?raw=true"
    chmod +x "dist/rpc.exe"

    echo "RPC binary download to dist/"
}