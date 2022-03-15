# Portainer Credential Helper
`docker-credential-portainer` is a credential helper accessing private registries from Edge stacks

When edge stacks are deployed in portainer, portainer will send down a list of credentials with the stack.

The edge agent holds these credentials in an in-memory database.  This credential helper will request them when required.
This binary is called by docker and docker-compose automatically.

# Usage

Place the `docker-credential-portainer` binary somewhere in the path.

Set the `credsStore` option to `portainer` in your `$HOME/.docker/config.json` file on Linux/Mac or `%USERPROFILE%/.docker/config.json` on Windows

```
{
  "credsStore": "portainer"
}
```


# Other
For details on the credential helper interface see: 
- [Docker login](https://docs.docker.com/engine/reference/commandline/login/)
- [Docker credential helpers](https://github.com/docker/docker-credential-helpers)
