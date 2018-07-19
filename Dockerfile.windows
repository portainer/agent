FROM microsoft/nanoserver

USER ContainerAdministrator

WORKDIR /app

COPY dist/agent.exe /app/

ENTRYPOINT ["C:/app/agent.exe"]
