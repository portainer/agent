#!/usr/bin/env bash

IMAGE_NAME=portainer-agent:develop
LOG_LEVEL=INFO
# PUBLIC_KEY=3059301306072a8648ce3d020106082a8648ce3d0301070342000447438d06749bd1946a6e48a97a669a8d09dd814e60b6df785fa93c59c5ef3c15cfbb717a58833a30bc690857cd4a69ee3e7afb24b88de7780208caecdd5cfced

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
-e PORTAINER_PUBKEY=${PUBLIC_KEY} \
--mode global \
--mount type=bind,src=//var/run/docker.sock,dst=/var/run/docker.sock \
--publish mode=host,target=9001,published=9001 \
--restart-condition none \
${IMAGE_NAME}

docker -H 10.0.7.10:2375 service logs -f pagent
