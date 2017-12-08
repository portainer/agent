# Portainer agent

Work in progress.


# Setup

1. Compile & Build

cd cmd/agent
CGO_ENABLED=0 go build -a --installsuffix cgo --ldflags '-s' && mv agent ../../dist/agent && docker build -t portainer/agent:develop -f ../../Dockerfile ../..

2. Start agent1: docker run --net agent -e AGENT_ADV_ADDR=172.18.0.2 portainer/agent:develop
3. Start agent2: docker run --net agent -e AGENT_ADV_ADDR=172.18.0.3 -e AGENT_CLUSTER_ADDR=172.18.0.2 portainer/agent:develop
