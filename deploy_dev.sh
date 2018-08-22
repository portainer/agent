#!/usr/bin/env bash

IMAGE_NAME=portainer/agent:local
LOG_LEVEL=DEBUG

if [[ $# -ne 1 ]] ; then
  echo "Usage: $(basename $0) <VERSION>"
  exit 1
fi

MODE=$1

function deploy_local() {
  echo "Cleanup previous settings..."
  docker rm -f pagent

  echo "Image build..."
  docker build --no-cache -t ${IMAGE_NAME} -f ../../Dockerfile ../..

  echo "Deployment..."
  docker run -d --name pagent \
  -e LOG_LEVEL=${LOG_LEVEL} \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -v /var/lib/docker/volumes:/var/lib/docker/volumes \
  -p 9001:9001 \
  portainer/agent:local

  docker logs -f pagent
}

function deploy_swarm() {
  echo "Cleanup previous settings..."

  docker -H 10.0.7.10:2375 service rm pagent
  docker -H 10.0.7.10:2375 network rm pagent-net
  docker -H 10.0.7.10:2375 rmi -f ${IMAGE_NAME}
  docker -H 10.0.7.11:2375 rmi -f ${IMAGE_NAME}

  echo "Image build..."

  docker -H 10.0.7.10:2375 build --no-cache -t ${IMAGE_NAME} -f ../../Dockerfile ../..
  docker -H 10.0.7.11:2375 build --no-cache -t ${IMAGE_NAME} -f ../../Dockerfile ../..

  echo "Sleep..."

  sleep 3

  echo "Deployment..."

  docker -H 10.0.7.10:2375 network create --driver overlay --attachable pagent-net
  docker -H 10.0.7.10:2375 service create --name pagent \
  --network pagent-net \
  -e LOG_LEVEL=${LOG_LEVEL} \
  -e AGENT_CLUSTER_ADDR=tasks.pagent \
  --mode global \
  --mount type=bind,src=//var/run/docker.sock,dst=/var/run/docker.sock \
  --mount type=bind,src=//var/lib/docker/volumes,dst=/var/lib/docker/volumes \
  --publish mode=host,target=9001,published=9001 \
  --restart-condition none \
  ${IMAGE_NAME}

  docker -H 10.0.7.10:2375 service logs -f pagent
}

function main() {

  mkdir dist
  cd cmd/agent

  echo "Compilation..."

  CGO_ENABLED=0 go build -a --installsuffix cgo --ldflags '-s'
  rc=$?; if [[ $rc != 0 ]]; then exit $rc; fi
  mv agent ../../dist/agent

  if [ ${MODE} == 'local' ]
  then
    deploy_local
  else
    deploy_swarm
  fi
}

main
