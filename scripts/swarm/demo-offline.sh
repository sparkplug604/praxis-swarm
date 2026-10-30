#!/usr/bin/env bash
# Run only existing test fixtures. No daemon, provider or model is started.
set -uo pipefail
umask 077

usage() {
  echo 'Usage: scripts/swarm/demo-offline.sh [--output-dir NEW_DIRECTORY]'
  echo 'Requires Go (backend/go.mod), Git and Python 3 on PATH.'
  echo 'Default output: a new /tmp/praxis-swarm-offline.* directory.'
}

output_dir=''
case "$#" in
  0) ;;
  1) if [[ "$1" == --help || "$1" == -h ]]; then usage; exit 0; else usage >&2; exit 2; fi ;;
  2) if [[ "$1" == --output-dir && -n "$2" ]]; then output_dir="$2"; else usage >&2; exit 2; fi ;;
  *) usage >&2; exit 2 ;;
esac

for tool in go git python3; do
  if ! command -v "$tool" >/dev/null 2>&1; then
    echo "Missing prerequisite on PATH: $tool" >&2
    exit 2
  fi
done
script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)" || exit 2
repo_root="$(cd -- "$script_dir/../.." && pwd -P)" || exit 2
pattern="$(python3 "$script_dir/summarize-demo.py" --pattern)" || exit 2

if [[ -n "$output_dir" ]]; then
  # mkdir is intentionally exclusive: never reuse, overwrite or remove evidence.
  mkdir -- "$output_dir" || exit 2
else
  output_dir="$(mktemp -d /tmp/praxis-swarm-offline.XXXXXX)" || exit 2
fi
output_dir="$(cd -- "$output_dir" && pwd -P)" || exit 2
printf 'Offline test evidence: %s\n' "$output_dir"
printf 'GOMAXPROCS=2 go -C %q test -json -count=1 -p 1 -run %q ./internal/service/swarm/scheduler\n' \
  "$repo_root/backend" "$pattern" > "$output_dir/command.txt" || exit 2

go_status=0
GOMAXPROCS=2 go -C "$repo_root/backend" test -json -count=1 -p 1 -run "$pattern" \
  ./internal/service/swarm/scheduler > "$output_dir/go-test.jsonl" 2> "$output_dir/go-test.stderr.log" || go_status=$?

report_status=0
python3 "$script_dir/summarize-demo.py" "$output_dir" "$go_status" || report_status=$?
printf 'Report: %s/report.md\nRaw events: %s/go-test.jsonl\nStderr: %s/go-test.stderr.log\n' \
  "$output_dir" "$output_dir" "$output_dir"
# Preserve Go's failure status, even if report generation also failed.
if (( go_status != 0 )); then exit "$go_status"; fi
exit "$report_status"
