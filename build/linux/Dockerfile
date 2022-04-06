FROM portainer/base

WORKDIR /app

COPY dist /app/
COPY static /app/static

# TODO: REVIEW
# Health check only available on Linux containers for the scope of the POC

# Health check could be reviewed by the platform team regarding the interval, timeout, start-period and retries
HEALTHCHECK --interval=10s --timeout=10s --start-period=5s --retries=1 CMD [ "/app/agent", "--health-check" ]

ENTRYPOINT ["./agent"]
