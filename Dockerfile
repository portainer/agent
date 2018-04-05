FROM portainer/base

WORKDIR /app

COPY dist/agent /app/

ENTRYPOINT ["./agent"]
