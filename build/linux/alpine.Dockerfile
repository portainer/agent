FROM alpine:latest

ENV PATH="/app:$PATH"
WORKDIR /app

COPY dist /app/
COPY static /app/static
RUN mkdir -p /root/.docker && mv /app/config.json /root/.docker/

ENTRYPOINT ["./agent"]
