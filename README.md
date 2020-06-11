# Portainer agent

## Purpose

The Portainer Agent is a workaround for a Docker API limitation when using the Docker API to manage a Docker environment. The user interactions with specific resources (containers, networks, volumes and images) are limited to those available on the node targeted by the Docker API request.

Docker Swarm mode introduces a concept which is the clustering of Docker nodes. It also adds services, tasks, configs and secrets which are cluster-aware resources. Cluster-aware means that you can query for a list of services or inspect a task inside any node on the cluster, as long as you’re executing the Docker API request on a manager node.

Containers, networks, volumes and images are node specific resources, not cluster-aware. When you, for example, want to list all the volumes available on a node inside your cluster, you will need to send a query to that specific node.

The purpose of the agent aims to allows previously node specific resources to be cluster-aware, all while keeping the Docker API request format. As aforementioned, this means that you only need to execute one Docker API request to retrieve all these resources from every node inside the cluster. In all bringing a better Docker user experience when managing Swarm clusters.

## Security

Here at Portainer, we believe in [responsible disclosure](https://en.wikipedia.org/wiki/Responsible_disclosure) of security issues. If you have found a security issue, please report it to <security@portainer.io>.

## Technical details

The Portainer agent is basically a cluster of Docker API proxies. Deployed inside a Swarm cluster on each node, it allows the
redirection (proxy) of a Docker API request on any specific node as well as the aggregration of the response of multiple nodes.

At startup, the agent will communicate with the Docker node it is deployed on via the Unix socket/Windows named pipe to retrieve information about the node (name, IP address, role in the Swarm cluster). This data will be shared when the agent will register into the agent cluster.

### Agent cluster

This implementation is using *serf* to form a cluster over a network, each agent requires an address where it will advertise its
ability to be part of a cluster and a join address where it will be able to reach other agents.

The agent retrieves the IP address it can use to create
a cluster by inspecting the Docker networks associated to the agent container. If multiple networks are available, it will pickup the first network available and retrieve the IP address inside this network.

Note: Be careful when deploying the agent to not deploy it inside the Swarm ingress network (by not using `mode=host` when exposing ports). This could lead to the agent being unable to create a cluster correctly, if picking the IP address inside the ingress network.

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

The agent also exposes the following endpoints:

* `/agents` (*GET*): List all the available agents in the cluster
* `/browse/ls` (*GET*): List the files available under a specific path on the filesystem
* `/browse/get` (*GET*): Retrieve a file available under a specific path on the filesytem
* `/browse/delete` (*DELETE*): Delete an existing file under a specific path on the filesytem
* `/browse/rename` (*PUT*): Rename an existing file under a specific path on the filesytem
* `/browse/put` (*POST*): Upload a file under a specific path on the filesytem
* `/host/info` (*GET*): Get information about the underlying host system
* `/ping` (*GET*): Returns a 204. Public endpoint that do not require any form of authentication
* `/key` (*GET*): Returns the Edge key associated to the agent **only available when agent is started in Edge mode**
* `/key` (*POST*): Set the Edge key on this agent **only available when agent is started in Edge mode**
* `/websocket/attach` (*GET*): Websocket attach endpoint (for container console usage)
* `/websocket/exec` (*GET*): Websocket exec endpoint (for container console usage)

Note: The `/browse/*` endpoints can be used to manage a filesystem. By default, it allows manipulation of files in Docker volumes (available under `/var/run/docker/volumes` when bind-mounted in the agent container) but can also manipulate files anywhere on the filesystem. 

### Agent API version

The agent API version is exposed via the `Portainer-Agent-API-Version` in each response of the agent.

## Using the agent in Edge mode

The following information is only relevant for an Agent that was started in Edge mode.

### Purpose

The Edge mode is mainly used in the case of your remote environment being not in the same network as your Portainer instance. When started in Edge mode, the agent will reach out to the Portainer instance
and will take care of creating a reverse tunnel allowing the Portainer instance to query it. It uses a token (Edge key) that contains the required information to connect to a specific Portainer instance.

### Startup

To start an agent in Edge mode, the `EDGE=1` environment variable must be set.

Upon startup, the agent will try to retrieve an existing Edge key in the following order:

* from the environment variables via the `EDGE_KEY` environment variable
* from the filesystem (see the Edge key section below for more information about key persistence on disk)
* from the cluster (if joining an existing Edge agent cluster)

If no Edge key was retrieved, the agent will start a HTTP server where it will expose a UI to associate an Edge key. After associating a key via the UI, the UI server will shutdown.

For security reasons, the Edge server UI will shutdown after 15 minutes if no key has been specified. The agent will require a restart in order
to access the Edge UI again.

### Edge key

The Edge key is used by the agent to connect to a specific Portainer instance. It is encoded using base64 and contains the following information:

* Portainer instance API URL
* Portainer instance tunnel server address
* Portainer instance tunnel server fingerprint
* Endpoint identifier

This information is represented in the following format before encoding (single string using the `|` character as a separator):

```
portainer_instance_url|tunnel_server_addr|tunnel_server_fingerprint|endpoint_ID
```

The Edge key associated to an agent will be persisted on disk after association under `/data/agent_edge_key`.

### Polling

After associating an Edge key to an agent, the agent will start polling the associated Portainer instance.

It will use the Portainer instance API URL and the endpoint identifier available in the Edge key to build the poll request URL: `http(s)://API_URL/api/endpoints/ENDPOINT_ID/status`

The response of the poll request contains the following information:

* Tunnel status
* Poll frequency
* Tunnel port
* Encrypted credentials
* Schedules

The tunnel status property can take one of the following values: `IDLE`, `REQUIRED`, `ACTIVE`. When this property is set to `REQUIRED`, the agent will
create a reverse tunnel to the Portainer instance using the port specified in the response as well as the credentials.

Each poll request sent to the Portainer instance contains the `X-PortainerAgent-EdgeID` header (with the value set to the Edge ID associated to the agent). This is used by the Portainer instance to associate an Edge ID to an endpoint so that an agent won't be able to poll information and join an Edge cluster by re-using an existing key without knowing the Edge ID.

To allow for pre-staged environments, this Edge ID is associated to an endpoint by Portainer after receiving the first poll request from an agent.

### Reverse tunnel

The reverse tunnel is established by the agent. The permissions associated to the credentials are set on the Portainer instance, the credentials are valid for a management session and can only be used
to create a reverse tunnel on a specific port (the one that is specified in the poll response).

The agent will monitor the usage of the tunnel. The tunnel will be closed in any of the following cases:

1. The status of the tunnel specified in the poll response is equal to `IDLE`
2. If no activity has been registered on the tunnel (no requests executed against the agent API) after a specific amount of time (can be configured via `EDGE_INACTIVITY_TIMEOUT`, default to 5 minutes)

### API server

When deployed in Edge mode, the agent API is not exposed over HTTPS anymore (see Using the agent non Edge section below) because we're using SSH to setup an encrypted tunnel. In order to avoid potential security issues with agent deployment exposing the API port on their host, the agent won't expose the API server under 0.0.0.0. Instead, it will expose the API server on the same IP address that is used to advertise the cluster (usually, the container IP in the overlay network).

This means that only a container deployed in the same overlay network as the agent will be able to query it.  

## Using the agent (non Edge)

The following information is only relevant for an Agent that was not started in Edge mode.

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

## Deployment options

The behavior of the agent can be tuned via a set of mandatory and optional options available as environment variables:

* AGENT_CLUSTER_ADDR (*mandatory*): address (in the IP:PORT format) of an existing agent to join the agent cluster. When deploying the agent as a Docker Swarm service,
we can leverage the internal Docker DNS to automatically join existing agents or form a cluster by using `tasks.<AGENT_SERVICE_NAME>:<AGENT_PORT>` as the address.
* AGENT_HOST (*optional*): address on which the agent API will be exposed (default to `0.0.0.0`)
* AGENT_PORT (*optional*): port on which the agent API will be exposed (default to `9001`)
* AGENT_SECRET (*optional*): shared secret used in the signature verification process
* LOG_LEVEL (*optional*): defines the log output verbosity (default to `INFO`)
* EDGE (*optional*): enable Edge mode. Disabled by default, set to `1` to enable it
* EDGE_KEY (*optional*): specify an Edge key to use at startup
* EDGE_ID (*mandatory when EDGE=1*): a unique identifier associated to this agent cluster
* EDGE_SERVER_HOST (*optional*): address on which the Edge UI will be exposed (default to `0.0.0.0`)
* EDGE_SERVER_PORT (*optional*): port on which the Edge UI will be exposed (default to `80`).
* EDGE_INACTIVITY_TIMEOUT (*optional*): timeout used by the agent to close the reverse tunnel after inactivity (default to `5m`)
* EDGE_INSECURE_POLL (*optional*): enable this option if you need the agent to poll a HTTPS Portainer instance with self-signed certificates. Disabled by default, set to `1` to enable it


For more information about deployment scenarios, see: https://portainer.readthedocs.io/en/stable/agent.html

## Development

1. Install go >= 1.11.2
2. Install dep: https://golang.github.io/dep/docs/installation.html

If you want to add any extra dependency:

```
dep ensure -add github.com/foo/bar
```

3. Run a local agent container:

```
./dev.sh local
```

4. Run the agent container inside a Swarm cluster (requires https://github.com/deviantony/vagrant-swarm-cluster)

```
./dev.sh swarm
```
