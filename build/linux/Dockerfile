FROM portainer/base

ENV PATH="/app:$PATH"
WORKDIR /app

COPY dist /app/
COPY static /app/static
COPY config $HOME/.docker/

# HEALTHCHECK --interval=10s --timeout=10s --start-period=5s --retries=1 CMD [ "/app/agent", "--health-check" ]

ENTRYPOINT ["./agent"]
