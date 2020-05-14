FROM microsoft/nanoserver:sac2016

USER ContainerAdministrator

WORKDIR /app

COPY dist /app/
COPY static /app/static

ENTRYPOINT ["C:/app/agent.exe"]
