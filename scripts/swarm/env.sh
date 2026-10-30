# Source with Bash. Override SWARM_RUNTIME_ROOT to reuse an existing local toolchain.
SWARM_REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
SWARM_RUNTIME_ROOT="${SWARM_RUNTIME_ROOT:-$SWARM_REPO_ROOT/.swarm-local}"
export SWARM_RUNTIME_ROOT
export PATH="$SWARM_RUNTIME_ROOT/npm/node_modules/.bin:$SWARM_RUNTIME_ROOT/node-v24.22.2-darwin-arm64/bin:$SWARM_RUNTIME_ROOT/go/bin:$PATH"
export GOPATH="$SWARM_RUNTIME_ROOT/gopath"
export GOMODCACHE="$SWARM_RUNTIME_ROOT/go-mod-cache"
export GOCACHE="$SWARM_RUNTIME_ROOT/go-build-cache"
export GOTOOLCHAIN=local
export npm_config_cache="$SWARM_RUNTIME_ROOT/npm-cache"
export npm_config_audit=false npm_config_fund=false npm_config_update_notifier=false
export electron_config_cache="$SWARM_RUNTIME_ROOT/electron-cache"
