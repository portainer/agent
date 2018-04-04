#!/usr/bin/env bash

IMAGE_NAME=portainer/agent:develop

cd cmd/agent

echo "Compilation..."

CGO_ENABLED=0 go build -a --installsuffix cgo --ldflags '-s'
rc=$?; if [[ $rc != 0 ]]; then exit $rc; fi
mv agent ../../dist/agent

echo "Image build..."

docker build -t ${IMAGE_NAME} -f ../../Dockerfile ../..
docker push ${IMAGE_NAME}
