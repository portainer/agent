FROM microsoft/nanoserver:sac2016

USER ContainerAdministrator

WORKDIR /app

COPY dist/agent.exe /app/

ENTRYPOINT ["C:/app/agent.exe"]
