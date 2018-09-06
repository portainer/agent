#!/usr/bin/env bash

IMAGE_NAME=portainer/agent:local
LOG_LEVEL=DEBUG
VAGRANT=true

if [[ $# -ne 1 ]] ; then
  echo "Usage: $(basename $0) <VERSION>"
  exit 1
fi

MODE=$1

function compile() {
  echo "Compilation..."
  ./build_in_container.sh linux amd64
}


function deploy_local() {
  echo "Cleanup previous settings..."

  docker rmi ${IMAGE_NAME}

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
  ##Assuming you're using the deviantony/vagrant-swarm-cluster.git setup

  if [ ["$VAGRANT"] ]
  then
    echo "Cleanup previous settings..."
    rm portainer-agent.tar
    DOCKER_MANAGER=10.0.7.10
    DOCKER_NODE=10.0.7.11
    docker -H $DOCKER_MANAGER:2375 service rm pagent
    docker -H $DOCKER_MANAGER:2375 network rm pagent-net
    docker -H $DOCKER_MANAGER:2375 rmi -f ${IMAGE_NAME}
    docker -H $DOCKER_NODE:2375 rmi -f ${IMAGE_NAME}

    echo "Image build..."
    ##First we build locally
    docker build --no-cache -t ${IMAGE_NAME} .
    docker save ${IMAGE_NAME} -o portainer-agent.tar
    docker -H $DOCKER_MANAGER:2375 load -i portainer-agent.tar
    docker -H $DOCKER_NODE:2375 load -i portainer-agent.tar

    echo "Sleep..."
    sleep 5

    echo "Deployment..."

    docker -H $DOCKER_MANAGER:2375 network create --driver overlay --attachable pagent-net
    docker -H $DOCKER_MANAGER:2375 service create --name pagent \
    --network pagent-net \
    -e LOG_LEVEL=${LOG_LEVEL} \
    -e AGENT_CLUSTER_ADDR=tasks.pagent \
    --mode global \
    --mount type=bind,src=//var/run/docker.sock,dst=/var/run/docker.sock \
    --mount type=bind,src=//var/lib/docker/volumes,dst=/var/lib/docker/volumes \
    --publish mode=host,target=9001,published=9001 \
    --restart-condition none \
    ${IMAGE_NAME}

    docker -H $DOCKER_MANAGER:2375 service logs -f pagent
  else
    echo "Unsupported configuration"
  fi
}

function main() {

  compile
  if [ ${MODE} == 'local' ]
  then
    deploy_local
  else
    deploy_swarm
  fi
}

main
