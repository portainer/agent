FROM portainer/base

ENV PATH="/app:$PATH"
WORKDIR /app

COPY dist /app/
COPY static /app/static
COPY config /root/.docker/

ENTRYPOINT ["./agent"]
