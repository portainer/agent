#!/usr/bin/env bash

IMAGE_NAME=portainer-agent:develop
LOG_LEVEL=INFO

cd cmd/agent

echo "Compilation..."

CGO_ENABLED=0 go build -a --installsuffix cgo --ldflags '-s'
rc=$?; if [[ $rc != 0 ]]; then exit $rc; fi
mv agent ../../dist/agent

echo "Image build..."

docker -H 10.0.7.10:2375 build -t ${IMAGE_NAME} -f ../../Dockerfile ../..
docker -H 10.0.7.11:2375 build -t ${IMAGE_NAME} -f ../../Dockerfile ../..
docker -H 10.0.7.12:2375 build -t ${IMAGE_NAME} -f ../../Dockerfile ../..

echo "Cleanup previous settings..."

docker -H 10.0.7.10:2375 service rm pagent
docker -H 10.0.7.10:2375 network rm pagent-net

echo "Sleep..."

sleep 7

echo "Swarm setup..."

docker -H 10.0.7.10:2375 network create --driver overlay pagent-net
docker -H 10.0.7.10:2375 service create --name pagent \
--network pagent-net \
-e LOG_LEVEL=${LOG_LEVEL} \
-e AGENT_CLUSTER_ADDR=tasks.pagent \
--mode global \
--mount type=bind,src=//var/run/docker.sock,dst=/var/run/docker.sock \
--publish mode=host,target=9001,published=9001 \
--restart-condition none \
${IMAGE_NAME}

docker -H 10.0.7.10:2375 service logs -f pagent
