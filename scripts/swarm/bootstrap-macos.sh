#!/bin/bash
# Pinned, local development tools. Does not install application dependencies.
set -euo pipefail
source "$(dirname "$0")/env.sh"
test "$(uname -sm)" = 'Darwin arm64' || { echo 'This bootstrap supports Apple Silicon macOS only.' >&2; exit 1; }
mkdir -p "$SWARM_RUNTIME_ROOT"
fetch_unpack() {
  local url="$1" name="$2" sha="$3" executable="$4"
  if test -x "$SWARM_RUNTIME_ROOT/$executable"; then return; fi
  curl --fail --location --proto '=https' --proto-redir '=https' --retry 2 "$url" -o "$SWARM_RUNTIME_ROOT/$name"
  printf '%s  %s\n' "$sha" "$SWARM_RUNTIME_ROOT/$name" | shasum -a 256 -c -
  tar -xzf "$SWARM_RUNTIME_ROOT/$name" -C "$SWARM_RUNTIME_ROOT"
}
fetch_unpack https://go.dev/dl/go1.27.1.darwin-arm64.tar.gz go1.27.1.darwin-arm64.tar.gz ee215d57e0ec269c60cc9ceca68e6bda321ba9ee5afe24f4b0988703c2d87d12 go/bin/go
fetch_unpack https://nodejs.org/dist/v24.22.2/node-v24.22.2-darwin-arm64.tar.gz node-v24.22.2-darwin-arm64.tar.gz aa9549349e47dffaa42dcf95d6e06ade8c4bdafc980979883b88767670df385a node-v24.22.2-darwin-arm64/bin/node
if ! test -f "$SWARM_RUNTIME_ROOT/npm/node_modules/npm/package.json"; then
  "$SWARM_RUNTIME_ROOT/node-v24.22.2-darwin-arm64/bin/npm" install --prefix "$SWARM_RUNTIME_ROOT/npm" --ignore-scripts --save-exact npm@10.9.9
fi
test "$(go version)" = 'go version go1.27.1 darwin/arm64'
test "$(node --version)" = 'v24.22.2'
test "$(npm --version)" = '10.9.9'
printf 'Pinned local toolchain ready in %s\n' "$SWARM_RUNTIME_ROOT"
