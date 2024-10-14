FROM alpine:latest

ENV PATH="/app:$PATH"
WORKDIR /app

COPY dist/agent /app/
COPY dist/docker /app/
COPY dist/docker-compose /app/
COPY dist/docker-credential-portainer /app/
COPY dist/kubectl /app/

COPY static /app/static
COPY config $HOME/.docker/

LABEL io.portainer.agent true \
    org.opencontainers.image.title="Portainer Agent" \
    org.opencontainers.image.description="The Portainer agent" \
    org.opencontainers.image.source="https://github.com/portainer/agent" \
    org.opencontainers.image.vendor="Portainer.io"

ENTRYPOINT ["./agent"]
