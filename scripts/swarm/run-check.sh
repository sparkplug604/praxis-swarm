#!/bin/bash
set -euo pipefail
source "$(dirname "$0")/env.sh"
# Runtime settings belong to the lab launch, not unit-test expectations.
for variable in $(compgen -e); do
  if [[ "$variable" == AO_* ]]; then unset "$variable"; fi
done
export GOMAXPROCS="${GOMAXPROCS:-2}"
check="${1:-}"
if (( $# )); then shift; fi
case "$check" in
  backend-build) cd "$SWARM_REPO_ROOT/backend"; exec go build -p 2 "$@" ./... ;;
  backend-race) cd "$SWARM_REPO_ROOT/backend"; exec go test -race -p 1 "$@" ./... ;;
  frontend-typecheck) cd "$SWARM_REPO_ROOT/frontend"; exec npm run typecheck -- "$@" ;;
  frontend-tests) cd "$SWARM_REPO_ROOT/frontend"; exec npm test -- --maxWorkers=1 "$@" ;;
  *) echo 'Usage: run-check.sh {backend-build|backend-race|frontend-typecheck|frontend-tests} [extra arguments]' >&2; exit 2 ;;
esac
