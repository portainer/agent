FROM scratch

WORKDIR /app

COPY dist/agent /app/

ENTRYPOINT ["./agent"]
