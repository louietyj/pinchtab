#!/usr/bin/env bash
# Build and publish a fork release: pinchtab-linux-amd64 (the headless-browser
# skill's sandbox), pinchtab-windows-amd64.exe (its Windows gatekeeper's
# bridge), and linux-arm64/darwin-arm64 builds, all stamped with the tag so
# either end can check it runs the other's build. See FORK.md.
#
# usage: scripts/fork-release.sh <N> <notes-file>    -> tag v0.15.2-capsolver.<N>
set -euo pipefail

N=${1:?usage: fork-release.sh <N> <notes-file>}
NOTES=${2:?usage: fork-release.sh <N> <notes-file>}
TAG=v0.15.2-capsolver.$N
cd "$(dirname "$0")/.."

[ "$(git rev-parse --abbrev-ref HEAD)" = main ] || { echo "not on main" >&2; exit 1; }
[ -z "$(git status --porcelain)" ] || { echo "working tree is dirty" >&2; exit 1; }
git fetch -q origin main
[ "$(git rev-parse HEAD)" = "$(git rev-parse origin/main)" ] || { echo "HEAD is not pushed" >&2; exit 1; }

ASSETS=(pinchtab-linux-amd64 pinchtab-linux-arm64 pinchtab-darwin-arm64 pinchtab-windows-amd64.exe)

build() { # <output>, named pinchtab-<goos>-<goarch>[.exe]
  local platform=${1#pinchtab-}; platform=${platform%.exe}
  MSYS_NO_PATHCONV=1 docker run --rm -v "$PWD":/src -v ptgomod:/go/pkg/mod -v ptgocache:/root/.cache/go-build -w /src \
    -e CGO_ENABLED=0 -e GOOS="${platform%-*}" -e GOARCH="${platform#*-}" golang:1.26 \
    go build -ldflags "-s -w -X main.version=$TAG" -o "/src/$1" ./cmd/pinchtab
}
for asset in "${ASSETS[@]}"; do build "$asset"; done

gh release create "$TAG" -R louietyj/pinchtab --target main \
  --title "v0.15.2 + CapSolver (fork build)" --notes-file "$NOTES" --latest \
  "${ASSETS[@]}"
rm -f "${ASSETS[@]}"
