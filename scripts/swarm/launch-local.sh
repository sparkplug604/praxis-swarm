#!/bin/bash
set -euo pipefail
source "$(dirname "$0")/env.sh"
SWARM_LAB_ROOT="${SWARM_LAB_ROOT:-$(mktemp -d /tmp/praxis-swm00.XXXXXX)}"
export AO_DATA_DIR="$SWARM_LAB_ROOT/data"
export AO_DEV_ELECTRON_DIR="$SWARM_LAB_ROOT/electron"
export AO_RUN_FILE="$AO_DATA_DIR/running.json"
export AO_DEV_DAEMON_BINARY="$SWARM_REPO_ROOT/frontend/daemon/ao"
export AO_ACP_RUNTIME_DIR="$SWARM_RUNTIME_ROOT/acp-not-installed"
export AO_HARNESS_UPDATE_CHECKS=off
export AO_TELEMETRY_EVENTS=off AO_TELEMETRY_METRICS=off AO_TELEMETRY_RENDERER=off
export AO_TELEMETRY_REMOTE=off AO_SENTRY_DSN=''
export AO_PORT="${SWARM_LOCAL_PORT:-43187}"
export AO_APP_RUN_ID=swm00-baseline
export AO_CLOUD_AUTH_REDIRECT="http://127.0.0.1:$AO_PORT/auth/callback"
unset AO_KEEP_DAEMON
umask 077
test -x "$AO_DEV_DAEMON_BINARY"
test -f "$SWARM_REPO_ROOT/frontend/node_modules/electron/path.txt"
if (( ${#AO_DATA_DIR} > 70 )); then
  echo 'Set SWARM_LAB_ROOT to a short private scratch directory (Unix socket path limit).' >&2
  exit 1
fi
mkdir -p "$AO_DATA_DIR" "$AO_DEV_ELECTRON_DIR" "$AO_ACP_RUNTIME_DIR"
python3 - "$AO_PORT" <<'PY'
import socket,sys
with socket.socket() as s:
    s.bind(('127.0.0.1',int(sys.argv[1])))
PY
cd "$SWARM_REPO_ROOT/frontend"
export ELECTRON_ENABLE_LOGGING=1
exec ./node_modules/.bin/electron-forge start
