FROM mcr.microsoft.com/windows/nanoserver:1809-amd64

USER ContainerAdministrator

WORKDIR /app

COPY dist /app/
COPY static /app/static

ENTRYPOINT ["C:/app/agent.exe"]
