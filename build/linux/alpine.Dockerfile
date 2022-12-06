FROM alpine:latest

ENV PATH="/app:$PATH"
WORKDIR /app

COPY dist /app/
COPY static /app/static
COPY config /root/.docker/

LABEL io.portainer.agent true

ENTRYPOINT ["./agent"]
