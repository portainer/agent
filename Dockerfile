FROM alpine

WORKDIR /app

COPY dist/agent /app/

ENTRYPOINT ["./agent"]
