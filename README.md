# Portainer agent

## Purpose

The Portainer Agent is a workaround for a Docker API limitation when using the Docker API to manage a Docker environment. The user interactions with specific resources (containers, networks, volumes and images) are limited to those available on the node targeted by the Docker API request.

Docker Swarm mode introduces a concept which is the clustering of Docker nodes. It also adds services, tasks, configs and secrets which are cluster-aware resources. Cluster-aware means that you can query for a list of services or inspect a task inside any node on the cluster, as long as you’re executing the Docker API request on a manager node.

Containers, networks, volumes and images are node specific resources, not cluster-aware. When you, for example, want to list all the volumes available on a node inside your cluster, you will need to send a query to that specific node.

The purpose of the agent aims to allows previously node specific resources to be cluster-aware, all while keeping the Docker API request format. As aforementioned, this means that you only need to execute one Docker API request to retrieve all these resources from every node inside the cluster. In all bringing a better Docker user experience when managing Swarm clusters.

## Technical details

The Portainer agent is basically a cluster of Docker API proxies. Deployed inside a Swarm cluster on each node, it allows the
redirection (proxy) of a Docker API request on any specific node as well as the aggregration of the response of multiple nodes.

At startup, the agent will communicate with the Docker node it is deployed on via the Unix socket/Windows named pipe to retrieve information about the node (name, IP address, role in the Swarm cluster). This data will be shared when the agent will register into the agent cluster.

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

By default, an agent will automatically generate its own set of TLS certificate and key. It will then use these to start the web
server where the agent API is exposed. By using self-signed certificates, each agent client and proxy will skip the TLS server verification when executing a request against another agent.

### Authentication

Each request to an agent must include a digital signature in the `X-PortainerAgent-Signature` header encoded using the `base64` format (without the padding characters).

![public key cryptography wikipedia](https://user-images.githubusercontent.com/5485061/48817100-ac410b80-eda9-11e8-8d72-ef668e8278df.png)

The following protocol is used between a Portainer instance and an agent:

For each HTTP request made from the Portainer instance to the agent:

1. The Portainer instance generates a signature using its private key. It encodes this signature in base64 and add it to the `X-PortainerAgent-Signature` header of the request
2. The Portainer instance encodes its public key in hexadecimal and adds it the `X-PortainerAgent-PublicKey` header of the request


For each HTTP request received from the agent:

1. The agent will check that the `X-PortainerAgent-PublicKey` and `X-PortainerAgent-Signature` headers are available in the request otherwise it returns a 403
2. The agent will then trigger the signature verification process. If the signature is not valid it returns a 403

#### Signature verification

The signature verification process can follow two different paths based on how the agent was deployed.

##### Default mode

By default, the agent will wait for a valid request from a Portainer instance and automatically associate the first Portainer instance that communicates with it by registering the public key found in the `X-PortainerAgent-PublicKey` header inside memory.

During the association process, the agent will first decode the specified public key from hexadecimal and then parse the public key. Only if these steps are successfull then the key will be associated to the agent.

Once a Portainer instance is registered by the agent, the agent will not try to decode/parse the public key associated to a request anymore and will assume that only signatures associated to this public key are authorized (preventing any other Portainer instance to communicate with this agent).

Finally, the agent uses the associated public key and a default message that is known by both entities to verify the signature available in the `X-PortainerAgent-Signature` header.

##### Secret mode

When the `AGENT_SECRET` environment variable is set in the execution context of the agent (`-e AGENT_SECRET=mysecret` when started as a container for example), the digital signature verification process will be slightly different.

In secret mode, the agent will not register a Portainer public key in memory anymore. Instead, it will **ALWAYS** decode and parse the public key available in the `X-PortainerAgent-PublicKey` and will then trigger the signature verification using key.

The signature verification is slightly altered as well, it now uses the public key sent in the request to verify the signature as well as the secret specified at startup in the `AGENT_SECRET` environment variable.

This mode will allow multiple instances of Portainer to connect to a single agent.

Note: Due to the fact that the agent will now decode and parse the public key associated to each request, this mode might be less performant than the default mode.


## Deployment

*This setup will assume that you're executing the following instructions on a Swarm manager node*

First thing to do, create an overlay network in which the agent will be deployed:

```
$ docker network create --driver overlay --attachable portainer_agent_network
```

Then, deploy the agent as a global service inside the previously created network:

```
$ docker service create --name portainer_agent \
--network portainer_agent_network \
-e AGENT_CLUSTER_ADDR=tasks.portainer_agent \
--mode global \
--mount type=bind,src=//var/run/docker.sock,dst=/var/run/docker.sock \
--constraint node.platform.os==linux \
portainer/agent:latest
```

```
docker run -d --name portainer_agent `
--restart always --network portainer_agent_network `
--label com.docker.stack.namespace=portainer `
-e AGENT_CLUSTER_ADDR=tasks.agent `
--mount type=npipe,source=\\.\pipe\docker_engine,target=\\.\pipe\docker_engine `
portainer/agent:latest
```

The last step is to connect Portainer to the agent.

If the Portainer instance is deployed inside the same overlay network as the agent then
Portainer can leverages the internal Docker DNS to automatically join any agent using `tasks.<AGENT_SERVICE_NAME>:<AGENT_PORT>`.

For example, based on the previous service deployment, `tasks.portainer_agent:9001` can be used as endpoint URL.

**IMPORTANT NOTE**: The agent is using HTTPS communication with self-signed certificates, any endpoint created inside the UI must
enable the `TLS` switch and use the `TLS only` option.

When creating the endpoint on the CLI using the `-H` Portainer flag, the `--tlsskipverify` flag must be specified, example: `-H tcp://tasks.portainer_agent:9001 --tlsskipverify`.

## Development

1. Install go >= 1.11.2
2. Install dep: https://golang.github.io/dep/docs/installation.html

If you want to add any extra dependency:

```go
dep ensure -add github.com/foo/bar
```
