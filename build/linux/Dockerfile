FROM portainer/base

ENV PATH="/app:$PATH"
WORKDIR /app

COPY dist /app/
COPY static /app/static
COPY config $HOME/.docker/

LABEL io.portainer.agent true

ENTRYPOINT ["./agent"]
