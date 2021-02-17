#!/usr/bin/env bash

function compile() {
    TARGET_DIST=dist/agent
    
    msg "Compilation..."
    
    cd cmd/agent || exit 1
    GOOS="linux" GOARCH="amd64" CGO_ENABLED=0 go build -a --installsuffix cgo --ldflags '-s'
    rc=$?; if [[ $rc != 0 ]]; then exit $rc; fi
    cd ../..
    mv cmd/agent/agent $TARGET_DIST
    
    msg "Agent executable is available on $TARGET_DIST"
}