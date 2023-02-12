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

# HEALTHCHECK --interval=10s --timeout=10s --start-period=5s --retries=1 CMD [ "/app/agent", "--health-check" ]
LABEL io.portainer.agent true

ENTRYPOINT ["./agent"]
