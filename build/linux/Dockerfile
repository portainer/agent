FROM portainer/base
COPY --from=busybox /bin/busybox /bin/
COPY bash /bin/

WORKDIR /app

COPY dist/agent /app/
COPY static /app/static

ENTRYPOINT ["./agent"]
