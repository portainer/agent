FROM portainer/base

WORKDIR /app

COPY dist /app/
COPY static /app/static

ENTRYPOINT ["./agent"]
