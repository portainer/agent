# Healthy

`healthy` is a lightweight CLI tool used to check the health status of a Portainer agent instance.

It performs a health check using the agent's internal logic and exits with a corresponding status code. This makes it ideal for use in automation, container health checks, and update orchestrators such as `portainer-updater`.

## How It Works

The tool leverages the `health.Healthy()` function to determine whether the agent can successfully communicate with the Portainer server.

- If the agent is **healthy**, a message is logged and the program exits with code `0`.
- If the agent is **not healthy**, an error is logged and the program exits with code `1`.

All log output is structured (JSON format) using [Zerolog](https://github.com/rs/zerolog), which makes it suitable for log aggregation systems.

## Usage

### Standalone
```sh
healthy
```
### In Docker
```sh
docker run --it portainer/agent:latest healthy
```
