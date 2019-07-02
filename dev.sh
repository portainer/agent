#!/usr/bin/env bash

LOG_LEVEL=DEBUG
CAP_HOST_MANAGEMENT=1 #Enabled by default. Change this to anything else to disable this feature
EDGE=1
EDGE_TUNNEL_SERVER="192.168.1.68"
VAGRANT=true
TMP="/tmp"
GIT_COMMIT_HASH=`git rev-parse --short HEAD`
GIT_BRANCH_NAME=`git rev-parse --abbrev-ref HEAD`
IMAGE_NAME="portainer/pagent:${GIT_BRANCH_NAME}-${GIT_COMMIT_HASH}"


if [[ $# -ne 1 ]] ; then
  echo "Usage: $(basename $0) <MODE:local/swarm>"
  exit 1
fi

MODE=$1

function compile() {
  echo "Compilation..."

  rm -rf dist/*
  cd cmd/agent
  GOOS="linux" GOARCH="amd64" CGO_ENABLED=0 go build -a --installsuffix cgo --ldflags '-s'
  rc=$?; if [[ $rc != 0 ]]; then exit $rc; fi
  cd ../..
  mv cmd/agent/agent dist/agent

}

function deploy_local() {
  EDGE_ID="4657f071-8a19-4102-abb6-02ddb8cf3468" # generated via uuidgen

  echo "Cleanup previous settings..."
  docker rm -f portainer-agent-dev
  docker rmi "${IMAGE_NAME}"

  echo "Image build..."
  docker build --no-cache -t "${IMAGE_NAME}" -f build/linux/Dockerfile .
#  docker push "${IMAGE_NAME}"


  echo "Deployment..."
  docker run -d --name portainer-agent-dev \
  -e LOG_LEVEL=${LOG_LEVEL} \
  -e CAP_HOST_MANAGEMENT=${CAP_HOST_MANAGEMENT} \
  -e EDGE=${EDGE} \
  -e EDGE_ID=${EDGE_ID} \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -v /var/lib/docker/volumes:/var/lib/docker/volumes \
  -v /:/host \
  -p 9001:9001 \
  -p 80:80 \
  "${IMAGE_NAME}"

  docker logs -f portainer-agent-dev
}

function deploy_swarm() {
  DOCKER_MANAGER=tcp://10.0.7.10
  DOCKER_NODE=tcp://10.0.7.11
#  DOCKER_NODE2=tcp://10.0.7.12
  EDGE_ID="a1a1c817-7f89-43b1-97e5-508d96c00be9" # generated via uuidgen


  echo "Cleanup previous settings..."

  rm "${TMP}/portainer-agent.tar"

  docker -H "${DOCKER_MANAGER}:2375" service rm portainer-agent-dev
  docker -H "${DOCKER_MANAGER}:2375" network rm portainer-agent-dev-net
  docker -H "${DOCKER_MANAGER}:2375" rmi -f "${IMAGE_NAME}"
  docker -H "${DOCKER_NODE}:2375" rmi -f "${IMAGE_NAME}"
#  docker -H "${DOCKER_NODE2}:2375" rmi -f "${IMAGE_NAME}"

  echo "Building image locally and exporting to Swarm cluster..."
  docker build --no-cache -t "${IMAGE_NAME}" -f build/linux/Dockerfile .
  docker save "${IMAGE_NAME}" -o "${TMP}/portainer-agent.tar"
  docker -H "${DOCKER_MANAGER}:2375" load -i "${TMP}/portainer-agent.tar"
  docker -H "${DOCKER_NODE}:2375" load -i "${TMP}/portainer-agent.tar"
#  docker -H "${DOCKER_NODE2}:2375" load -i "${TMP}/portainer-agent.tar"

  echo "Sleep..."
  sleep 5

  echo "Deployment..."

  docker -H "${DOCKER_MANAGER}:2375" network create --driver overlay --attachable portainer-agent-dev-net
  docker -H "${DOCKER_MANAGER}:2375" service create --name portainer-agent-dev \
  --network portainer-agent-dev-net \
  -e LOG_LEVEL="${LOG_LEVEL}" \
  -e CAP_HOST_MANAGEMENT=${CAP_HOST_MANAGEMENT} \
  -e EDGE=${EDGE} \
  -e EDGE_ID=${EDGE_ID} \
  -e AGENT_CLUSTER_ADDR=tasks.portainer-agent-dev \
  --mode global \
  --mount type=bind,src=//var/run/docker.sock,dst=/var/run/docker.sock \
  --mount type=bind,src=//var/lib/docker/volumes,dst=/var/lib/docker/volumes \
  --mount type=bind,src=//,dst=/host \
  --publish mode=host,target=9001,published=9001 \
  --publish mode=host,published=80,target=80 \
  --restart-condition none \
  "${IMAGE_NAME}"

#  --mount type=volume,src=portainer_agent_data,dst=/data \

  docker -H "${DOCKER_MANAGER}:2375" service logs -f portainer-agent-dev
}

function main() {
  compile

  if [[ "${MODE}" == 'local' ]]; then
    deploy_local
  else
    # Only to be used with deviantony/vagrant-swarm-cluster.git
    deploy_swarm
  fi
}

main
