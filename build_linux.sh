#!/bin/sh
set -eu
cd "$(dirname "$0")"

mkdir -p build
export CGO_ENABLED=0

echo "Building build/bb-mp-server-linux-amd64..."
GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o build/bb-mp-server-linux-amd64 ./cmd/beatblock-mp

echo "Building build/bb-mp-server-linux-arm64..."
GOOS=linux GOARCH=arm64 go build -trimpath -ldflags="-s -w" -o build/bb-mp-server-linux-arm64 ./cmd/beatblock-mp

echo "Done: build/bb-mp-server-linux-amd64, build/bb-mp-server-linux-arm64"
