#!/usr/bin/env sh
# Builds dist/Hestia-Setup.exe (the app, which installs itself) for 64-bit Windows from any OS with Go 1.24+.
# Usage: ./build.sh [version]     e.g. ./build.sh 1.1.0
set -eu
cd "$(dirname "$0")"
VERSION="${1:-dev}"
case "$VERSION" in
  [0-9]*.[0-9]*.[0-9]*) WINVER="$VERSION.0" ;;
  *) WINVER="0.0.0.0" ;;
esac
go run github.com/tc-hib/go-winres@v0.3.3 make --in winres/winres.json --arch amd64 \
  --file-version "$WINVER" --product-version "$WINVER"
mkdir -p dist
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath \
  -ldflags "-s -w -H windowsgui -X main.appVersion=$VERSION" -o dist/Hestia-Setup.exe .
rm -f rsrc_windows_amd64.syso
cd dist
if command -v sha256sum >/dev/null; then sha256sum Hestia-Setup.exe > SHA256SUMS.txt; else shasum -a 256 Hestia-Setup.exe > SHA256SUMS.txt; fi
cat SHA256SUMS.txt
