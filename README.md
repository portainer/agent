# Portainer agent

## Purpose

The purpose of the agent is to work around a Docker API limitation. When using the
Docker API to manage a Docker environment, the user interactions with specific resources
(containers, networks, volumes and images) are limited to these available on the node targeted by the
Docker API request.

Docker Swarm mode introduce the concept of cluster of Docker nodes. With that concept, it
also introduces the services, tasks, configs and secrets which are cluster aware resources.
This means that you can query for the list of service or inspect a task inside any node on the cluster
as long as you're executing the Docker API request on a manager node.

Containers, networks, volumes and images are node specific resources, not cluster aware.
If you want to get the list of all the volumes available on the node number 3 inside your cluster,
you need to execute the request to query the volumes on that specific node.

The agent purpose aims to solve that issue and make the containers, networks and volumes resources cluster aware while
keeping the Docker API request format.

This means that you only need to execute one Docker API request to retrieve all the volumes inside your cluster for example.

The final goal is to bring a better Docker UX when managing Swarm clusters.

## Technical details

The Portainer agent is basically a cluster of Docker API proxies. Deployed inside a Swarm cluster on each node, it allows the
redirection (proxy) of a Docker API request on any specific node as well as the aggregration of the response of multiple nodes.

At startup, the agent will communicate with the Docker node it is deployed on via the Unix socket (**not available on Windows yet**) to retrieve information about the node (name, IP address, role in the Swarm cluster). This data will be shared when the agent will register into the agent cluster.

### Agent cluster

This implementation is using *serf* to form a cluster over a network, each agent requires an address where it will advertise its
ability to be part of a cluster and a join address where it will be able to reach other agents.

### Proxy

The agent works as a proxy to the Docker API on which it is deployed as well as a proxy to the other agents inside the cluster.

In order to proxy the request to the other agents inside the cluster, it introduces a header called `X-PortainerAgent-Target` which can have
the name of any node in the cluster as a value. When this header is specified, the Portainer agent receiving the request will extract its value, retrieve the address of the agent located on the node specified using this header value and proxy the request to it.

If no header `X-PortainerAgent-Target` is present, we assume that the agent receiving the request is the target of the request and it will
be proxied to the local Docker API.

Some requests are specifically marked to be executed against a manager node inside the cluster (`/services/**`, `/tasks/**`, `/nodes/**`, etc... e.g. all the Docker API operations that can only be executed on a Swarm manager node). This means that you can execute these requests
against any agent in the cluster and they will be proxied to an agent (and to the associated Docker API) located on a Swarm manager node.

### Proxy & aggregation

By default, the agent will inspect any requests and search for the `X-PortainerAgent-ManagerOperation` header. If it is found (the value of the header does not matter),
then the agent will proxy that request to an agent located on any manager node. If this header is not found, then an operation will be executed based on the endpoint prefix (`/networks` for a cluster operation, `/services` for a manager operation, etc...).

The `X-PortainerAgent-ManagerOperation` header was introduced to work-around the fact that a Portainer instance uses the Docker CLI binary to manage stacks and the binary
**MUST** always target a manager node when executing any command.

Some requests are specifically marked to be executed against the whole cluster:

* `GET /containers/json`
* `GET /images/json`
* `GET /volumes`
* `GET /networks`

The agent handles these requests using the same header mechanism. If no `X-PortainerAgent-Target` is found, it will automatically execute
the request against each node in the cluster in a concurrent way. Behind the scene, it retrieves the IP address of each node, create a copy of the request, decorate each request with the `X-PortainerAgent-Target` header and aggregate the response of each request into a single one (reproducing the Docker API response format).


### Docker API compliance

When communicating with a Portainer agent instead of using the Docker API directly, the only difference is the possibility to add the `X-PortainerAgent-Target` header to each request to be able to execute some actions against a specific node in the cluster.

The fact that the agent final proxy target is always the Docker API means that we keep the Docker original response format. The only difference in the response is that the agent will automatically add the `Portainer-Agent` header to each response using the version of the Portainer agent as value.

### Agent specific API

The agent exposes the following endpoints:

* `/agents` (*GET*): Returns the list of all the available agents in the cluster

## Security

### Encryption

By default, each node will automatically generate its own set of TLS certificate and key. It will then use these to start the web
server where the agent API is exposed. By using self-signed certificates, each agent client and proxy will skip the TLS server verification when executing a request against another agent.

### Authentication

Each request to an agent must include a digital signature in the `X-PortainerAgent-Signature` header. The signature is generated using a private key in the Portainer instance and included in each request. The agent uses the public key of the Portainer instance to verify if the signature is valid.

For convenience, the Portainer public key is always included inside the `X-PortainerAgent-PublicKey` header in each request to the agent. The first time the agent will
find the `X-PortainerAgent-PublicKey` header in a request, it will automatically register the public key contained in the header and will stop looking at this header.

If no public key is registered and the agent cannot find the `X-PortainerAgent-PublicKey` header in a request, it will return a 403. If a public key is registered and
the agent cannot find the `X-PortainerAgent-Signature` header or that the header contains an invalid signature, it will return a 403.

## Deployment

*This setup will assume that you're executing the following instructions on a Swarm manager node*

First thing to do, create an overlay network in which the agent will be deployed:

```
$ docker network create --driver overlay portainer_agent_network
```

Then, deploy the agent as a global service inside the previously created network:

```
$ docker service create --name portainer_agent \
--network portainer_agent_network \
-e AGENT_CLUSTER_ADDR=tasks.portainer_agent \
--mode global \
--mount type=bind,src=//var/run/docker.sock,dst=/var/run/docker.sock \
portainer/agent:develop
```

The last step is to connect Portainer to the agent.

If the Portainer instance is deployed inside the same overlay network as the agent then
Portainer can leverages the internal Docker DNS to automatically join any agent using `tasks.<AGENT_SERVICE_NAME>:<AGENT_PORT>`.

For example, based on the previous service deployment, `tasks.portainer_agent:9001` can be used as endpoint URL.

**IMPORTANT NOTE**: The agent is using HTTPS communication with self-signed certificates, any endpoint created inside the UI must
enable the `TLS` switch and use the `TLS only` option.

When creating the endpoint on the CLI using the `-H` Portainer flag, the `--tlsskipverify` flag must be specified, example: `-H tcp://tasks.portainer_agent:9001 --tlsskipverify`.
