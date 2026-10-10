#!/usr/bin/env bash
# Build and publish a fork release: pinchtab-linux-amd64 (the headless-browser
# skill's sandbox) and pinchtab-windows-amd64.exe (its Windows gatekeeper's
# bridge), both stamped with the tag so either end can check it runs the
# other's build. See FORK.md.
#
# usage: scripts/fork-release.sh <N> <notes-file>    -> tag v0.15.2-capsolver.<N>
set -euo pipefail

N=${1:?usage: fork-release.sh <N> <notes-file>}
NOTES=${2:?usage: fork-release.sh <N> <notes-file>}
TAG=v0.15.2-capsolver.$N
cd "$(dirname "$0")/.."

[ "$(git rev-parse --abbrev-ref HEAD)" = capsolver-rebase ] || { echo "not on capsolver-rebase" >&2; exit 1; }
[ -z "$(git status --porcelain)" ] || { echo "working tree is dirty" >&2; exit 1; }
git fetch -q origin capsolver-rebase
[ "$(git rev-parse HEAD)" = "$(git rev-parse origin/capsolver-rebase)" ] || { echo "HEAD is not pushed" >&2; exit 1; }

build() { # <goos> <output>
  MSYS_NO_PATHCONV=1 docker run --rm -v "$PWD":/src -v ptgomod:/go/pkg/mod -v ptgocache:/root/.cache/go-build -w /src \
    -e CGO_ENABLED=0 -e GOOS="$1" -e GOARCH=amd64 golang:1.26 \
    go build -ldflags "-s -w -X main.version=$TAG" -o "/src/$2" ./cmd/pinchtab
}
build linux pinchtab-linux-amd64
build windows pinchtab-windows-amd64.exe

gh release create "$TAG" -R louietyj/pinchtab --target capsolver-rebase \
  --title "v0.15.2 + CapSolver (fork build)" --notes-file "$NOTES" --latest \
  pinchtab-linux-amd64 pinchtab-windows-amd64.exe
rm -f pinchtab-linux-amd64 pinchtab-windows-amd64.exe
