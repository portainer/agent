ARG OSVERSION
FROM --platform=linux/amd64 gcr.io/k8s-staging-e2e-test-images/windows-servercore-cache:1.0-linux-amd64-${OSVERSION} AS core

FROM mcr.microsoft.com/windows/nanoserver:${OSVERSION}
ENV PATH="C:\mingit\cmd;C:\app;C:\Windows\system32;C:\Windows;"

COPY --from=core /Windows/System32/netapi32.dll /Windows/System32/netapi32.dll

USER ContainerAdministrator

COPY dist/mingit/ mingit/
COPY dist/agent.exe /app/
COPY dist/docker.exe /app/
COPY dist/docker-compose.exe /app/
COPY dist/docker-credential-portainer.exe /app/
COPY dist/kubectl.exe /app/

COPY static /app/static
COPY config /Users/ContainerAdministrator/.docker/

# HEALTHCHECK --interval=10s --timeout=10s --start-period=5s --retries=1 CMD [ "C:/app/agent", "--health-check" ]
LABEL io.portainer.agent=true

ENTRYPOINT ["C:/app/agent.exe"]