#!/bin/bash

# very much just copied from https://lemariva.com/blog/2019/12/portainer-managing-docker-engine-remotely
# production use should involve a real external certificate management system

## TESTING CERT ROTATION
## 1. run this script with COUNT param of 2 to generate 2 client certs
##    * update CUSTOM_IP to match your setup and generate the EDGE_KEY for step 4
##
## 2. launch openssl server with
## CPATH=~/.config/portainer/certs ; openssl s_server -accept 9443 -CAfile $CPATH/server-cert.pem  -cert $CPATH/server-cert.pem -key $CPATH/server-key.pem -state -strict -Verify 1
##
## 3. copy first client certs with
## CPATH=~/.config/portainer/ cp ~$CPATH/certs1/* $CPATH/certs
##
## 4. start edge agent with
## CPATH=~/.config/portainer/certs; docker run --rm -v /var/run/docker.sock:/var/run/docker.sock -v /var/lib/docker/volumes:/var/lib/docker/volumes -e EDGE=1 -e EDGE_ID=f7702b01-0851-48dc-887-b10c24ef65 -e EDGE_KEY=aHR0cHM6Ly8xOTIuMTY4LjEuMjA6OTQ0M3wxOTIuMTY4LjEuMjA6ODAwMHw2NDpmMDo4ZDo4NTpmZDoyYjo3MDo0YTowZDpmNjpjNzpmMzo0YjozNDo0N3w0 -e CAP_HOST_MANAGEMENT=1 -v $CPATH:/certs --name portainer_edge_agent portainerci/agent:pr289 --sslcert /certs/agent-cert.pem --sslkey /certs/agent-key.pem --sslcacert /certs/ca-cert.pem
##
## 5. see in server logs the cert displayed on agent handshake
##
## 6. don't stop edge agent and hot rotate the new certs with
## CPATH=~/.config/portainer/ cp ~$CPATH/certs2/* $CPATH/certs
##
## 7. wait for the new auto handshake logs in server logs
##
## 8. compare with the previous displayed cert. If rotation/auto pickup on edge was successful, logged cert should be different than the previous one

CURRENT=$(pwd)
HOST=${1:-"portainer.p1.alho.st"}
CERTDIR=~/.config/portainer/certs/

CUSTOM_IPS='IP:192.168.1.20'

declare -A _=(
  [ca]='ca'
  [server]='server'
  [client]='agent'
  [cert]='cert'
  [key]='key'
  [ext]='extfile'
)

declare -A ca=(
  [cert]=${_[ca]}-${_[cert]}.pem
  [key]=${_[ca]}-${_[key]}.pem
)

declare -A server=(
  [cert]=${_[server]}-${_[cert]}.pem
  [key]=${_[server]}-${_[key]}.pem
  [ext]=${_[server]}-${_[ext]}.cnf
  [csr]=${_[server]}.csr
)

declare -A client=(
  [cert]=${_[client]}-${_[cert]}.pem
  [key]=${_[client]}-${_[key]}.pem
  [ext]=${_[client]}-${_[ext]}.cnf
  [csr]=${_[client]}.csr
)

info() {
  echo ""
  echo "$1"
}

gen_ca() {
  encryption=${1:-'-sha256'}

  info " > > > Generate the CA cert < < <"
  openssl genrsa -aes256 -out ${ca[key]} 4096
  # enter a pass phrase to protect the ca-key

  openssl req -new -x509 -days 365 ${encryption} -key ${ca[key]} -out ${ca[cert]}
}

gen_server_cert() {
  DEFAULT_IPS='IP:10.0.0.200,IP:127.0.0.1,IP:10.10.10.189'

  info " > > > Generate the Portainer server cert < < <"
  openssl genrsa -out ${server[key]} 4096

  openssl req -sha256 -new \
    -subj "/CN=${HOST}" \
    -key ${server[key]} \
    -out ${server[csr]}

  echo "subjectAltName = DNS:${HOST},${DEFAULT_IPS},${CUSTOM_IPS}" >>${server[ext]}
  echo "extendedKeyUsage = serverAuth" >>${server[ext]}

  openssl x509 -req -sha256 -days 365 \
    -CAcreateserial \
    -CA ${ca[cert]} \
    -CAkey ${ca[key]} \
    -extfile ${server[ext]} \
    -in ${server[csr]} \
    -out ${server[cert]}

  info " > > > Done: Generate the Portainer server cert < < <"
}

gen_client_cert() {
  dir=${1:-'.'}
  [[ $dir != '.' ]] && mkdir $dir

  info " > > > Generate an agent cert < < <"
  openssl genrsa -out ${client[key]} 4096

  openssl req -new \
    -subj '/CN=client' \
    -key ${client[key]} \
    -out ${client[csr]}

  echo "extendedKeyUsage = clientAuth" >${client[ext]}

  openssl x509 -req -days 365 -sha256 \
    -CAcreateserial \
    -CA ${ca[cert]} \
    -CAkey ${ca[key]} \
    -extfile ${client[ext]} \
    -in ${client[csr]} \
    -out ${client[cert]}

  [[ $dir != '.' ]] && mv ${_[client]}* $dir
  info " > > > Done: Generate an agent cert < < <"
}

count=${1:-'1'}
[[ $# == 1 ]] && shift

re='^[0-9]+$'
if ! [[ $count =~ $re ]]; then
  echo "Usage: $(basename $0) [COUNT]"
  echo "       - COUNT: number of clients certs to generate"
  echo "                defaults to 1"
  exit 1
fi

mkdir -p ${CERTDIR}
cd ${CERTDIR} || exit
info " > > > Generating example mTLS certs into $(pwd)"

([[ ! -f "${ca[cert]}" ]] && gen_ca) || info " - ${ca[cert]} CA cert already exists"

([[ ! -f "${server[cert]}" ]] && gen_server_cert) || info " - ${server[cert]} cert already exists"

for ((i = 1; i <= $count; i++)); do
  dir="../certs$i"
  if [[ $i == 1 && $count == 1 ]]; then
    dir='.'
  fi
  ([[ ! -f "$dir/${client[cert]}" ]] && gen_client_cert $dir) || info " - $dir/${client[cert]} cert already exists"
done

info " > > > Done: Generating example mTLS certs into $(pwd)"
cd "${CURRENT}" || return
