#!/usr/bin/env sh
# Builds Hestia.exe for Windows (64-bit) from any OS with Go 1.24+.
set -eu
cd "$(dirname "$0")"
go run github.com/tc-hib/go-winres@v0.3.3 make --in winres/winres.json --arch amd64
mkdir -p dist
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -H windowsgui" -o dist/Hestia.exe .
rm -f rsrc_windows_amd64.syso
sha256sum dist/Hestia.exe 2>/dev/null || shasum -a 256 dist/Hestia.exe
