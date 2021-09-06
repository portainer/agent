#!/usr/bin/env bash

local=0
podman=0
swarm=0
build=0
compile=0
swarm_ips=()
edge=0
edge_id=""
edge_key=""

LOG_LEVEL=DEBUG

function deploy_command() {
    parse_deploy_params "${@:1}"
    local ret_value=""
    
    default_image_name
    local IMAGE_NAME=$ret_value
    
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
    
    
    if [[ "$local" == "1" ]]; then
        deploy_local
    fi
    
    if [[ "$swarm" == "1" ]]; then
        deploy_swarm "${swarm_ips[@]}"
    fi
}

function deploy_local() {
    if [[ "$swarm" == "1" ]]; then
        run_swarm
        exit 0
    fi
    
    msg "Running local agent $IMAGE_NAME"
    
    docker rm -f portainer-agent-dev
    
    docker run -d --name portainer-agent-dev \
    -e LOG_LEVEL=${LOG_LEVEL} \
    -e EDGE=${edge} \
    -e EDGE_ID="${edge_id}" \
    -e EDGE_KEY="${edge_key}" \
    -v /var/run/docker.sock:/var/run/docker.sock \
    -v /var/lib/docker/volumes:/var/lib/docker/volumes \
    -v /:/host \
    -p 9001:9001 \
    -p 80:80 \
    "${IMAGE_NAME}"
    
    docker logs -f portainer-agent-dev
}

function deploy_podman() {
    msg "Running local agent $IMAGE_NAME with podman socket"
    
    #podman rm -f portainer-agent-dev
    
    # Create local folder for podman volumes
    mkdir -p /run/user/1000/podman/myvolumes
    
    podman run -d --name portainer-agent-dev \
    -e LOG_LEVEL=${LOG_LEVEL} \
    -e PODMAN=1 \
    -v /run/user/1000/podman/podman.sock:/var/run/docker.sock \
    -v /run/user/1000/podman/myvolumes:/var/lib/docker/volumes \
    -v /:/host \
    -p 9001:9001 \
    -p 8080:80 \
    "${IMAGE_NAME}"
    
    podman logs -f portainer-agent-dev
}

function deploy_swarm() {
    if [ $# -eq 0 ]; then
        die "Swarm expects at least manager ip"
    fi
    
    run_swarm "$@"
}

function run_swarm() {
    local URL_PREFIX=""
    if [ $# -ne 0 ]; then
        URL_PREFIX="-H "${1}:2375""
    fi
    
    local node_ips=()
    if [ $# -gt 1 ]; then
        node_ips=("${@:2}")
    fi
    
    
    msg "Cleaning..."
    rm "/tmp/portainer-agent.tar" || true
    
    docker "$URL_PREFIX" service rm portainer-agent-dev || true
    docker "$URL_PREFIX" network rm portainer-agent-dev-net || true
    
    for node_ip in "${node_ips[@]}"; do
        docker -H "${node_ip}:2375" rmi -f "${IMAGE_NAME}" || true
    done
    
    msg "Exporting image to Swarm cluster..."
    docker save "${IMAGE_NAME}" -o "/tmp/portainer-agent.tar"
    docker "$URL_PREFIX" load -i "/tmp/portainer-agent.tar"
    
    for node_ip in "${node_ips[@]}"; do
        docker -H "${node_ip}:2375" load -i "/tmp/portainer-agent.tar"
    done
    
    docker "$URL_PREFIX" network create --driver overlay portainer-agent-dev-net
    docker "$URL_PREFIX" service create --name portainer-agent-dev \
    --network portainer-agent-dev-net \
    -e LOG_LEVEL="${LOG_LEVEL}" \
    -e EDGE=${edge} \
    -e EDGE_ID="${edge_id}" \
    -e EDGE_KEY="${edge_key}" \
    --mode global \
    --mount type=bind,src=//var/run/docker.sock,dst=/var/run/docker.sock \
    --mount type=bind,src=//var/lib/docker/volumes,dst=/var/lib/docker/volumes \
    --mount type=bind,src=//,dst=/host \
    --publish target=9001,published=9001 \
    --publish mode=host,published=80,target=80 \
    "${IMAGE_NAME}"
    
    docker "$URL_PREFIX" service logs -f portainer-agent-dev
}

function parse_deploy_params() {
    if [[ "${1-}" == "" ]]; then
        usage_deploy
    fi
    
    while :; do
        case "${1-}" in
            -h | --help) usage_deploy ;;
            -v | --verbose)
                msg "verbose"
                set -x
            ;;
            --local) local=1 ;;
            -s | --swarm) swarm=1 ;;
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
            --ip)
                swarm_ips+=("$2")
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
    cmd="./dev.sh"
    cat <<EOF
Usage: $cmd deploy [-h] [-v|--verbose] [--local] [-s|--swarm]
        [-c|--compile] [-b|--build] [-e|--edge] [--edge-id EDGE_ID] [--edge-key EDGE_KEY]
        [--ip SWARM_MANAGER_IP] [--ip SWARM_NODE_IP1] [--ip SWARM_NODE_IP2]

This script is intended to help with deploying of dev enviroment

Available flags:
-h, --help                              Print this help and exit
-v, --verbose                           Verbose output
--local                                 Deploy to a local docker
-s, --swarm                             Deploy to a swarm cluster
-p, --podman                            Deploy to a local podman
-c, --compile                           Compile the code before deployment (will also build a docker image)
-b, --build                             Build the image before deployment
-e, --edge                              Deploy an edge agent
--edge-e, --edge  [EDGE_ID EDGE_KEY]    Deploy an edge agent
id EDGE_ID                              Set agent edge id to EDGE_ID (required when using -e without edge-id)
--edge-key EDGE_KEY                     Set agent edge key to EDGE_KEY
--ip IP                                 Swarm IP, the first will always be the manager ip
EOF
    exit
}
