FROM portainer/base

WORKDIR /app

COPY dist /app/
COPY static /app/static

HEALTHCHECK --interval=10s --timeout=5s --start-period=3s --retries=1 CMD [ "/app/agent", "--health-check" ]

ENTRYPOINT ["./agent"]
