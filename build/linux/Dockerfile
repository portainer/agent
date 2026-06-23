FROM portainer/base

ENV PATH="/app:$PATH"
WORKDIR /app

COPY dist/agent /app/
COPY dist/docker-credential-portainer /app/
COPY dist/healthy /app/

COPY static /app/static
COPY config /root/.docker/

ARG GIT_COMMIT=unspecified
ARG BUILD_DATE=unspecified
LABEL git_commit=$GIT_COMMIT \
  org.opencontainers.image.revision=$GIT_COMMIT \
  org.opencontainers.image.created=$BUILD_DATE \
  org.opencontainers.image.title="Portainer Agent" \
  org.opencontainers.image.description="Portainer agent for managing environments." \
  org.opencontainers.image.vendor="Portainer.io" \
  org.opencontainers.image.url="https://www.portainer.io" \
  org.opencontainers.image.documentation="https://docs.portainer.io" \
  io.portainer.agent="true"

ENTRYPOINT ["./agent"]
