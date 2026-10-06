#!/usr/bin/env sh
# Cross-compile dist/MantisWatcher.exe from macOS/Linux (Go 1.22+).
set -e
cd "$(dirname "$0")"
VERSION="${1:-2.0.0}"
mkdir -p dist
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath \
  -ldflags "-s -w -H windowsgui -X main.version=$VERSION" \
  -o dist/MantisWatcher.exe ./cmd/mantis-watcher
echo "built dist/MantisWatcher.exe"
