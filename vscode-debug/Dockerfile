FROM golang:1.21-alpine

RUN go install github.com/go-delve/delve/cmd/dlv@latest

ENV PATH="/app:$PATH"
WORKDIR /app

COPY dist/agent /app/
COPY dist/docker /app/
COPY dist/docker-compose /app/
COPY dist/docker-credential-portainer /app/
COPY dist/kubectl /app/

COPY static /app/static
COPY config $HOME/.docker/

LABEL io.portainer.agent true

ENTRYPOINT /go/bin/dlv --listen=0.0.0.0:50100 --api-version=2 --headless=true --only-same-user=false --accept-multiclient --check-go-version=false exec /app/agent
