ARG OSVERSION
FROM --platform=linux/amd64 gcr.io/k8s-staging-e2e-test-images/windows-servercore-cache:1.0-linux-amd64-${OSVERSION} AS core

FROM mcr.microsoft.com/windows/nanoserver:${OSVERSION}
ENV PATH="C:\mingit\cmd;C:\app;C:\Windows\system32;C:\Windows;"

COPY --from=core /Windows/System32/netapi32.dll C:/Windows/System32/netapi32.dll

USER ContainerAdministrator

COPY dist/mingit/ C:/mingit/
COPY dist/agent.exe C:/app/
COPY dist/docker.exe C:/app/
COPY dist/docker-compose.exe C:/app/
COPY dist/docker-credential-portainer.exe C:/app/
COPY dist/kubectl.exe C:/app/

# COPY static/ C:/app/static
COPY config /Users/ContainerAdministrator/.docker/

LABEL io.portainer.agent=true

ENTRYPOINT ["C:/app/agent.exe"]