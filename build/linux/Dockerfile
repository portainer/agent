FROM portainer/base

WORKDIR /app

COPY dist/agent /app/
COPY static /app/static

ENTRYPOINT ["./agent"]
