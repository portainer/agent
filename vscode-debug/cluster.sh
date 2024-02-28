#!/bin/bash

cluster_name='vscode-debug'

# find script dir, following symlinks if any
SOURCE=${BASH_SOURCE[0]}
while [ -L "$SOURCE" ]; do # resolve $SOURCE until the file is no longer a symlink
  DIR=$(cd -P "$(dirname "$SOURCE")" >/dev/null 2>&1 && pwd)
  SOURCE=$(readlink "$SOURCE")
  [[ $SOURCE != /* ]] && SOURCE=$DIR/$SOURCE # if $SOURCE was a relative symlink, we need to resolve it relative to the path where the symlink file was located
done
SCRIPT_DIR=$(cd -P "$(dirname "$SOURCE")" >/dev/null 2>&1 && pwd)

create() {
  kind create cluster --config=$SCRIPT_DIR/kind.yaml --name $cluster_name
}

delete() {
  kind delete clusters $cluster_name
}

recreate() {
  delete
  create
}

help() {
  cat <<EOF

Usage: ${0} CMD

  with CMD:
    - create: create the kind cluster
    - delete: delete the cluster
    - recreate: recreate the cluster
    - * (anything else): show this help
EOF
}

if [[ $# -ne 1 ]]; then
  help
  exit
fi

cmd=$1

case $cmd in
create | delete | recreate)
  $cmd
  ;;
*)
  help
  ;;
esac
