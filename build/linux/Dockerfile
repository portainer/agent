FROM portainer/base

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

ENTRYPOINT ["./agent"]
