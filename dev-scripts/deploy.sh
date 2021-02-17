local=0
swarm=0
build=0
compile=0
swarm_ips=()
edge=0
edge_id=""
edge_key=""

LOG_LEVEL=DEBUG

function parse_params() {
  while :; do
    case "${1-}" in
      -h | --help) usage ;;
      -v | --verbose) set -x ;;
      --no-color) NO_COLOR=1 ;;
      --local) local=1 ;; 
      -s | --swarm) swarm=1 ;; 
      -c | --compile) compile=1 ;; 
      -b | --build) build=1 ;; 
      -e | --edge) edge=1 ;; 
      --edge-id)
        edge_id=$2
        shift
        ;;
      --edge-key)
        edge_key=$2
        shift
        ;;
      --ip)
        swarm_ips+=($2) 
        shift 
        ;;
      -?*) die "Unknown option: $1" ;;
      *) break ;;
    esac
    shift
  done

  # args=("$@")
  
  # # check required params and arguments
  # [[ -z "${param-}" ]] && die "Missing required parameter: param"
  # [[ ${#args[@]} -eq 0 ]] && die "Missing script arguments"

  if [[ ($edge -eq 1) && (-z "${edge_id}") ]];
  then
    die "Missing edge id"
  fi
  # echo ips ${swarm_ips[@]}
  # echo swarm $swarm
  # echo "local" $local
  # echo swarm_manager_ip $swarm_manager_ip
  return 0
}


function deploy() {
  parse_params ${@:2}
  local ret_value=""

  default_image_name
  local IMAGE_NAME=$ret_value

  if [[ "$compile" == "1" ]];
  then
    compile
    build=1
  fi

  if [[ "$build" == "1" ]];
  then
    build $IMAGE_NAME
  fi

  if [[ "$local" == "1" ]];
  then
    deploy_local
  fi

  if [[ "$swarm" == "1" ]];
  then
    deploy_swarm ${swarm_ips[@]}
  fi
}

function deploy_local() {
  if [[ "$swarm" == "1" ]];
  then
    run_swarm
    exit 0
  fi 

  msg "Running local agent $IMAGE_NAME"

  docker rm -f portainer-agent-dev

  docker run -d --name portainer-agent-dev \
    -e LOG_LEVEL=${LOG_LEVEL} \
    -e EDGE=${edge} \
    -e EDGE_ID=${edge_id} \
    -e EDGE_KEY=${edge_key} \
    -v /var/run/docker.sock:/var/run/docker.sock \
    -v /var/lib/docker/volumes:/var/lib/docker/volumes \
    -v /:/host \
    -p 9001:9001 \
    -p 80:80 \
    "${IMAGE_NAME}"

  docker logs -f portainer-agent-dev
}


function deploy_swarm() {
  if [ $# -eq 0 ]
  then
    die "Swarm expects at least manager ip"
  fi
  
  run_swarm $@
}

function run_swarm() {
  local URL_PREFIX=""
  if [ $# -ne 0 ]
  then
    URL_PREFIX="-H "${1}:2375""
  fi

  local node_ips=()
  if [ $# -gt 1 ]
  then
    node_ips=${@:2}
  fi

  rm "/tmp/portainer-agent.tar" || true

  msg "Cleaning..."
  docker $URL_PREFIX service rm portainer-agent-dev || true
  docker $URL_PREFIX network rm portainer-agent-dev-net || true

  for node_ip in ${node_ips[@]};
  do
    docker -H "${node_ip}:2375" rmi -f "${IMAGE_NAME}" || true
  done

  msg "Exporting image to Swarm cluster..."
  docker save "${IMAGE_NAME}" -o "/tmp/portainer-agent.tar"
  docker $URL_PREFIX load -i "/tmp/portainer-agent.tar"

  for node_ip in ${node_ips[@]};
  do
    docker -H "${node_ip}:2375" load -i "/tmp/portainer-agent.tar"
  done
  

  docker $URL_PREFIX network create --driver overlay portainer-agent-dev-net
  docker $URL_PREFIX service create --name portainer-agent-dev \
    --network portainer-agent-dev-net \
    -e LOG_LEVEL="${LOG_LEVEL}" \
    -e EDGE=${edge} \
    -e EDGE_ID=${edge_id} \
    -e EDGE_KEY=${edge_key} \
    --mode global \
    --mount type=bind,src=//var/run/docker.sock,dst=/var/run/docker.sock \
    --mount type=bind,src=//var/lib/docker/volumes,dst=/var/lib/docker/volumes \
    --mount type=bind,src=//,dst=/host \
    --publish target=9001,published=9001 \
    --publish mode=host,published=80,target=80 \
    "${IMAGE_NAME}"

  docker $URL_PREFIX service logs -f portainer-agent-dev
}