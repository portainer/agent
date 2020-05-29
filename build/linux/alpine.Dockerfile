FROM alpine:latest

WORKDIR /app

COPY dist /app/
COPY static /app/static

ENTRYPOINT ["./agent"]
