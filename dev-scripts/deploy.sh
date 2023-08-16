#!/usr/bin/env bash
#!/bin/bash

# strict mode - based on http://redsymbol.net/articles/unofficial-bash-strict-mode/
set -euo pipefail
IFS=$'\n\t'

mode='' # standalone or swarm
podman=0
build=0
compile=0
ips=()
edge=0
edge_id=""
edge_key=""
edge_async=0
image_name=""
env_vars=()

LOG_LEVEL=DEBUG

DOCKER_PORT=2376
CERTS_PATH="/home/baron_l/.docker/machine/certs"
function docker(){
  command docker --tls=true --tlscacert=$CERTS_PATH/ca.pem --tlscert=$CERTS_PATH/cert.pem --tlskey=$CERTS_PATH/key.pem "$@"
}


function deploy_command() {
    parse_deploy_params "${@:1}"
    
    local IMAGE_NAME=$image_name
    if [[ -z "$image_name" ]]; then
        local ret_value=""
        default_image_name
        IMAGE_NAME=$ret_value
    fi
    deploy
}

function deploy() {
    
    if [[ "$compile" == "1" ]]; then
        compile
        build=1
    fi
    
    if [[ "$podman" == "1" ]]; then
        if [[ "$build" == "1" ]]; then
            build_podman "$IMAGE_NAME"
        fi
        
        deploy_podman
        exit 0
    fi
    
    if [[ "$build" == "1" ]]; then
        build "$IMAGE_NAME"
    fi
    
    url=""
    if [ ${#ips[@]} -ne 0 ]; then
        url="${ips[0]}:$DOCKER_PORT"
    fi
    
    if [ -z "$mode" ] || [ "$mode" == "standalone" ]; then
        deploy_standalone "$url"
    fi
    
    if [[ "$mode" == "swarm" ]]; then
        deploy_swarm "$url"
    fi
}


function load_image() {
    local image_name=$1
    local url=${2:-''}
    local node_ips=("${@:3}")
    
    if [ -z "$url" ]; then
        return 0
    fi
    
    msg "Exporting image to machine..."
    docker save "${image_name}" -o "/tmp/portainer-agent.tar"
    
    docker -H "$url" rmi -f "${IMAGE_NAME}" || true
    docker -H "$url" load -i "/tmp/portainer-agent.tar"
    
    if [ ${#node_ips[@]} -eq 0 ]; then
        return 0
    fi
    
    msg "Exporting image to nodes..."
    for node_ip in "${node_ips[@]}"; do
        docker -H "${node_ip}:2375" rmi -f "${IMAGE_NAME}" || true
        docker -H "${node_ip}:2375" load -i "/tmp/portainer-agent.tar"
    done
}

function deploy_standalone() {
    local url=${1:-""}
    msg "Running standalone agent $IMAGE_NAME"
    
    CONTAINER_NAME="${CONTAINER_NAME:-"portainer-agent-dev"}"

    docker -H "$url" rm -f "$CONTAINER_NAME" || true
    
    load_image "$IMAGE_NAME" "$url"
    
    cmd=(docker)

    if [ -n "$url" ]; then
        cmd+=(-H "$url")
    fi

    cmd+=(run -d --name "$CONTAINER_NAME")
    cmd+=(-v /var/run/docker.sock:/var/run/docker.sock)
    cmd+=(-v /var/lib/docker/volumes:/var/lib/docker/volumes)
    cmd+=(-v /:/host)
    cmd+=(-e LOG_LEVEL="${LOG_LEVEL}")

    if [[ "$edge" == "1" ]]; then
        cmd+=(-e EDGE=1)
        cmd+=(-e EDGE_ID="$edge_id")
        cmd+=(-e EDGE_ASYNC="$edge_async")
        cmd+=(-e EDGE_INSECURE_POLL=1)

        if [ -n "$edge_key" ]; then
            cmd+=(-e EDGE_KEY="$edge_key")
        else 
            cmd+=(-p 80:80)
        fi
    else 
        cmd+=(-p 9001:9001)
    fi

    for env_var in "${env_vars[@]}"; do
        cmd+=(-e "$env_var")
    done

    cmd+=(--add-host=host.docker.internal:host-gateway)

    cmd+=("$IMAGE_NAME")

    "${cmd[@]}"
    
    docker -H "$url" logs -f "$CONTAINER_NAME"
}

function deploy_podman() {
    msg "Running local agent $IMAGE_NAME with podman socket"
    
    #podman rm -f portainer-agent-dev
    
    # Create local folder for podman volumes
    mkdir -p /run/user/1000/podman/myvolumes
    
    podman run -d --name "${CONTAINER_NAME:-"portainer-agent-dev"}" \
    -e LOG_LEVEL=${LOG_LEVEL} \
    -e PODMAN=1 \
    -v /run/user/1000/podman/podman.sock:/var/run/docker.sock \
    -v /run/user/1000/podman/myvolumes:/var/lib/docker/volumes \
    -v /:/host \
    -p 9001:9001 \
    -p 8080:80 \
    "${IMAGE_NAME}"
    
    podman logs -f "${CONTAINER_NAME:-"portainer-agent-dev"}"
}

function deploy_swarm() {
    if [ $# -eq 0 ]; then
        die "Swarm expects a manager ip"
    fi
    
    msg "Deploying swarm"
    
    local url="$1"
    
    local node_ips=()
    if [[ ${#ips[@]} -gt 1 ]]; then
        node_ips=("${ips[@]:1}")
    fi
    
    msg "Cleaning..."
    rm "/tmp/portainer-agent.tar" || true
    
    docker -H "$url" service rm portainer-agent-dev || true
    docker -H "$url" network rm portainer-agent-dev-net || true
    # load_image "$IMAGE_NAME" "$url" "${node_ips[@]}"
    
    sleep 2
    
    docker -H "$url" network create --driver overlay portainer-agent-dev-net

    cmd=(docker)
    
    if [ -n "$url" ]; then
        cmd+=(-H "$url")
    fi

    cmd+=(service create --name "${CONTAINER_NAME:-"portainer-agent-dev"}")
    cmd+=(--network portainer-agent-dev-net)
    cmd+=(-e LOG_LEVEL="${LOG_LEVEL}")

    if [[ "$edge" == "1" ]]; then
        cmd+=(-e EDGE=1)
        cmd+=(-e EDGE_ID="$edge_id")
        cmd+=(-e EDGE_ASYNC="$edge_async")
        cmd+=(-e EDGE_INSECURE_POLL=1)

        if [ -n "$edge_key" ]; then
            cmd+=(-e EDGE_KEY="$edge_key")
        else 
            cmd+=(-p 80:80)
        fi
    else 
        cmd+=(-p 9001:9001)
    fi

    for env_var in "${env_vars[@]}"; do
        cmd+=(-e "$env_var")
    done

    cmd+=(--host host.docker.internal:host-gateway)
    cmd+=(--mode global)
    cmd+=(--mount "type=bind,src=//var/run/docker.sock,dst=/var/run/docker.sock")
    cmd+=(--mount "type=bind,src=//var/lib/docker/volumes,dst=/var/lib/docker/volumes")
    cmd+=(--mount "type=bind,src=//,dst=/host")
    cmd+=("${IMAGE_NAME}")
    
    echo ">>>>"
    echo "${cmd[@]}"

    # "${cmd[@]}"

    # docker -H "$url" service logs -f portainer-agent-dev
}

function parse_deploy_params() {
    while :; do
        case "${1-}" in
            -h | --help) usage_deploy ;;
            -v | --verbose)
                msg "verbose"
                set -x
            ;;
            --standalone) mode='standalone' ;;
            -s | --swarm) mode='swarm' ;;
            -p | --podman) podman=1 ;;
            -c | --compile) compile=1 ;;
            -b | --build) build=1 ;;
            -e | --edge)
                edge=1
                if [[ $# -ge 2 && ! $2 == -* ]]; then # id is supplied
                    edge_id=$2
                    if [[ $# -ge 3 && (! $3 == -*)]]; then # key is supplied
                        edge_key=$3
                        shift
                    fi
                    shift
                fi
            ;;
            --edge-id)
                edge_id=$2
                shift
            ;;
            --edge-key)
                edge_key=$2
                shift
            ;;
            --edge-async)
                edge_async=1
            ;;
            --ip)
                ips+=("$2")
                shift
            ;;
            --env)
                env_vars+=("$2")
                shift
            ;;
            --image-name)
                image_name=$2
                shift
            ;;
            -?*) die "Unknown option: $1" ;;
            *) break ;;
        esac
        shift
    done
    
    if [[ ($edge -eq 1) && (-z "${edge_id}") ]]; then
        die "Missing edge id"
    fi
    
    return 0
}

function usage_deploy() {
    local cmdPath="./dev.sh"
    cat <<EOF
Usage: $cmdPath deploy [-h] [-v|--verbose] [--local] [-s|--swarm]
        [-c|--compile] [-b|--build] [-e|--edge] [--edge-id EDGE_ID] [--edge-key EDGE_KEY]
        [--ip SWARM_MANAGER_IP] [--ip SWARM_NODE_IP1] [--ip SWARM_NODE_IP2]

This script is intended to help with deploying of dev environment

Available flags:
-h, --help                              Print this help and exit
-v, --verbose                           Verbose output
-s, --swarm                             Deploy to a swarm cluster (by default deploys to standalone)
-p, --podman                            Deploy to a local podman
-c, --compile                           Compile the code before deployment (will also build a docker image)
-b, --build                             Build the image before deployment
-e, --edge  [EDGE_ID EDGE_KEY]          Deploy an edge agent (optional with EDGE_ID and EDGE_KEY)
--edge-id EDGE_ID                       Set agent edge id to EDGE_ID (required when using -e without edge-id)
--edge-key EDGE_KEY                     Set agent edge key to EDGE_KEY
--edge-async                            Enable EDGE_ASYNC mode
--ip IP                                 can be provided zero, once or more times. for standalone only the first ip is considered
                                            for swarm the first ip is the manager and the rest are nodes
EOF
    exit
}
