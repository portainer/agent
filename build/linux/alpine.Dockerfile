FROM alpine:latest

WORKDIR /app

COPY dist /app/
COPY static /app/static

HEALTHCHECK --interval=5s --timeout=5s --start-period=10s --retries=3 CMD [ "/app/agent", "--health-check" ]

ENTRYPOINT ["./agent"]
