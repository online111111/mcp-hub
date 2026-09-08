#!/usr/bin/env bash
set -euo pipefail
version="${1#v}"
if [[ ! "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  echo 'Expected a stable semantic version such as v0.4.5' >&2
  exit 1
fi
release_sha="$(git rev-parse HEAD)"
grep -Fq "Version   = \"${version}\"" internal/buildinfo/buildinfo.go
build_date="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
mkdir -p dist
stage="$(mktemp -d)"
trap 'rm -rf "$stage"' EXIT
for target in linux/amd64 linux/arm64 windows/amd64 windows/arm64 darwin/amd64 darwin/arm64; do
  goos="${target%/*}"
  goarch="${target#*/}"
  name="mcp-manager-${version}-${goos}-${goarch}"
  bin='mcp-manager'
  if [ "$goos" = windows ]; then bin='mcp-manager.exe'; fi
  rm -f "$stage"/*
  CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" go build -trimpath \
    -ldflags="-s -w -X github.com/online111111/mcp-manager/internal/buildinfo.Version=${version} -X github.com/online111111/mcp-manager/internal/buildinfo.Commit=${release_sha} -X github.com/online111111/mcp-manager/internal/buildinfo.BuildDate=${build_date}" \
    -o "$stage/$bin" ./cmd/mcp-manager
  cp README.md "$stage/README.md"
  if [ "$goos" = windows ]; then
    cp scripts/windows/start-mcp-manager.cmd scripts/windows/start-mcp-manager.ps1 \
      scripts/windows/copy-admin-token.cmd scripts/windows/copy-admin-token.ps1 \
      scripts/windows/README-WINDOWS.txt "$stage/"
    rm -f "dist/${name}.zip"
    (cd "$stage" && zip -q "$OLDPWD/dist/${name}.zip" "$bin" README.md start-mcp-manager.cmd start-mcp-manager.ps1 copy-admin-token.cmd copy-admin-token.ps1 README-WINDOWS.txt)
  else
    tar -C "$stage" -czf "dist/${name}.tar.gz" "$bin" README.md
  fi
done
(cd dist && sha256sum "mcp-manager-${version}-"* > SHA256SUMS)
