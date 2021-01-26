FROM mcr.microsoft.com/windows/servercore:ltsc2019 as core

ENV GIT_VERSION 2.30.0
ENV GIT_PATCH_VERSION 2



RUN setx /M Path "c:\git\cmd;c:\git\bin;c:\git\usr\bin;%Path%;c:\gcc\bin;c:\7zip"

# RUN powershell -Command $ErrorActionPreference = 'Stop' ; \
#     [Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12 ; \
#     Invoke-WebRequest $('https://github.com/git-for-windows/git/releases/download/v{0}.windows.{1}/MinGit-{0}.{1}-busybox-64-bit.zip' -f $env:GIT_VERSION, $env:GIT_PATCH_VERSION) -OutFile 'mingit.zip' -UseBasicParsing ; \
#     Expand-Archive mingit.zip -DestinationPath c:\mingit

RUN powershell -command Invoke-WebRequest -outfile portableGit.7z.exe https://github.com/git-for-windows/git/releases/download/v2.26.0.windows.1/PortableGit-2.26.0-64-bit.7z.exe
RUN powershell -command Invoke-WebRequest -UserAgent 'DockerCI' -outfile 7zsetup.exe http://www.7-zip.org/a/7z1514-x64.exe
RUN powershell -command start-process .\7zsetup.exe -ArgumentList '/S /D=c:/7zip' -Wait

RUN powershell 7z x portableGit.7z.exe -ogit

FROM mcr.microsoft.com/windows/nanoserver:1809-amd64

USER ContainerAdministrator

COPY --from=core /windows/system32/netapi32.dll /windows/system32/netapi32.dll
COPY --from=core "c:\git" "c:\git"

WORKDIR /app

COPY dist /app/
COPY static /app/static


RUN setx /M Path "C:\git\bin;C:\git\usr\bin;C:\git\mingw64\bin;%Path%"

RUN git version

ENTRYPOINT ["C:/app/agent.exe"]
