@echo off
setlocal
cd /d "%~dp0"

if not exist build mkdir build

set CGO_ENABLED=0
set GOOS=windows
set GOARCH=amd64

echo Building build\bb-mp-server.exe...
go build -trimpath -ldflags="-s -w" -o build\bb-mp-server.exe ./cmd/beatblock-mp
if errorlevel 1 (
    echo Build failed.
    pause
    exit /b 1
)

echo Done: build\bb-mp-server.exe
