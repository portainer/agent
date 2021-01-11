FROM mcr.microsoft.com/windows/servercore:ltsc2019 as core
FROM mcr.microsoft.com/windows/nanoserver:1809-amd64

USER ContainerAdministrator

COPY --from=core /windows/system32/netapi32.dll /windows/system32/netapi32.dll

WORKDIR /app

COPY dist /app/
COPY static /app/static

ENTRYPOINT ["C:/app/agent.exe"]
