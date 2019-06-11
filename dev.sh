#!/usr/bin/env bash

IMAGE_NAME=portainer/pagent:edge
LOG_LEVEL=DEBUG
CAP_HOST_MANAGEMENT=1 #Enabled by default. Change this to anything else to disable this feature
EDGE=1
EDGE_TUNNEL_SERVER="192.168.1.68"
#AGENT_SECRET="bG9jYWxob3N0OjgwMDA6Nzc3NzpjZjplZDo0YTo3YzplOTo1YTpjMTphNzo4MTphOTpkNjoyNDpiMzoyMDphOTphMjphZ2VudEByYW5kb21zdHJpbmc"
#AGENT_SECRET="bG9jYWxob3N0OjgwMDA6NjM0MTk6Y2Y6ZWQ6NGE6N2M6ZTk6NWE6YzE6YTc6ODE6YTk6ZDY6MjQ6YjM6MjA6YTk6YTI6YWdlbnRAcmFuZG9tc3RyaW5n"
VAGRANT=true
TMP="/tmp"


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

function build_edge() {
    echo "Building..."

    echo "Building image locally and exporting to Vagrant node..."
    docker build --no-cache -t "${IMAGE_NAME}" -f build/linux/Dockerfile .
#    docker push "${IMAGE_NAME}"
    docker save "${IMAGE_NAME}" -o "${TMP}/portainer-agent.tar"
    docker -H "10.0.10.10:2375" rmi "${IMAGE_NAME}"
    docker -H "10.0.10.10:2375" load -i "${TMP}/portainer-agent.tar"

    deploy_local
}

function deploy_local() {
  echo "Cleanup previous settings..."
  docker -H "10.0.10.10:2375" rm -f portainer-agent-dev

  echo "Image build..."
#  docker build --no-cache -t "${IMAGE_NAME}" -f build/linux/Dockerfile .

  echo "Deployment..."
  docker -H "10.0.10.10:2375" run -d --name portainer-agent-dev \
  -e LOG_LEVEL=${LOG_LEVEL} \
  -e CAP_HOST_MANAGEMENT=${CAP_HOST_MANAGEMENT} \
  -e EDGE=${EDGE} \
  -e EDGE_TUNNEL_SERVER=${EDGE_TUNNEL_SERVER} \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -v /var/lib/docker/volumes:/var/lib/docker/volumes \
  -v /:/host \
  -p 9001:9001 \
  -p 80:80 \
  "${IMAGE_NAME}"

  docker -H "10.0.10.10:2375" logs -f portainer-agent-dev
}

function deploy_swarm() {
  DOCKER_MANAGER=tcp://10.0.7.10
  DOCKER_NODE=tcp://10.0.7.11
#  DOCKER_NODE2=tcp://10.0.7.12

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
  -e EDGE_TUNNEL_SERVER=${EDGE_TUNNEL_SERVER} \
  -e AGENT_CLUSTER_ADDR=tasks.portainer-agent-dev \
  --mode global \
  --mount type=bind,src=//var/run/docker.sock,dst=/var/run/docker.sock \
  --mount type=bind,src=//var/lib/docker/volumes,dst=/var/lib/docker/volumes \
  --mount type=bind,src=//,dst=/host \
  --publish mode=host,target=80,published=80 \
  --restart-condition none \
  "${IMAGE_NAME}"

  docker -H "${DOCKER_MANAGER}:2375" service logs -f portainer-agent-dev
}

function main() {

  compile
  if [ "${MODE}" == 'local' ]; then
    deploy_local
  elif [ "${MODE}" == 'edge' ]; then
    build_edge
  else
    # Only to be used with deviantony/vagrant-swarm-cluster.git
    deploy_swarm
  fi
}

main
