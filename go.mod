module github.com/portainer/agent

go 1.15

require (
	github.com/Microsoft/go-winio v0.4.17
	github.com/armon/go-metrics v0.0.0-20190430140413-ec5e00d3c878 // indirect
	github.com/asaskevich/govalidator v0.0.0-20190424111038-f61b66f89f4a
	github.com/containerd/containerd v1.5.2 // indirect
	github.com/docker/docker v17.12.0-ce-rc1.0.20180816081446-320063a2ad06+incompatible
	github.com/docker/go-connections v0.4.0 // indirect
	github.com/docker/spdystream v0.0.0-20181023171402-6480d4af844c // indirect
	github.com/gorilla/mux v1.7.2
	github.com/gorilla/websocket v1.4.2
	github.com/hashicorp/go-immutable-radix v1.1.0 // indirect
	github.com/hashicorp/go-msgpack v0.5.5 // indirect
	github.com/hashicorp/go-sockaddr v1.0.2 // indirect
	github.com/hashicorp/logutils v1.0.0
	github.com/hashicorp/memberlist v0.1.4 // indirect
	github.com/hashicorp/serf v0.8.3
	github.com/jaypipes/ghw v0.0.0-20181115172816-cebc09458380
	github.com/jaypipes/pcidb v0.0.0-20181115143611-141a53e65d4a // indirect
	github.com/jpillora/chisel v0.0.0-20190128092258-ee6601a6bbde
	github.com/koding/websocketproxy v0.0.0-20181220232114-7ed82d81a28c
	github.com/miekg/dns v1.1.14 // indirect
	github.com/morikuni/aec v1.0.0 // indirect
	github.com/pkg/errors v0.9.1
	github.com/portainer/docker-compose-wrapper v0.0.0-20210329220935-8d56f0cb5735
	github.com/portainer/libcompose v0.5.3
	github.com/portainer/libcrypto v0.0.0-20190723020511-2cfe5519d14f
	github.com/portainer/libhttp v0.0.0-20190806161840-cde6e97fcd52
	k8s.io/api v0.20.6
	k8s.io/client-go v0.20.6
)

replace github.com/docker/docker => github.com/docker/engine v1.4.2-0.20200204220554-5f6d6f3f2203

replace golang.org/x/sys => golang.org/x/sys v0.0.0-20190826190057-c7b8b68b1456

replace github.com/Microsoft/go-winio => github.com/Microsoft/go-winio v0.4.14
