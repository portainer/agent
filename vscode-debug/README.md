# How to debug the agent on kubernetes

## Prerequisites

- Create kind cluster

```sh
`make debug/cluster/create`
```

- Download dependencies & build other binaries

```sh
make all
```

- Create a task in `.vscode/launch.json`

```json
{
  "name": "Debug Remote",
  "type": "go",
  "request": "attach",
  "mode": "remote",
  "host": "localhost",
  "port": 50100,
  "cwd": "${workspaceFolder}",
  "trace": "verbose"
}
```

- Start the debug edge agent (you can follow the state and logs in a browser tab by pressing `Space` once the tilt process has started)

```sh
make debug/up
```

- Launch your debug session in VSCode
  - you can minimize the VSCode debug window, all logs will happen in tilt UI