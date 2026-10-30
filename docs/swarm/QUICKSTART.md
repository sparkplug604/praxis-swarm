# Run the offline developer preview

For real model execution, use the separate [supervised CLI guide](SUPERVISED_WORKFLOW.md).
The [corrected connected workflow](SUPERVISED_WORKFLOW_RESULT.md) completed two
workers on one Mac and passed independent frozen-code checks.

This demo runs existing coordination tests, not AI workers. It needs no provider
account, API key, Node.js, Electron, Docker, or private cloud module.

## Prerequisites

Use macOS with Bash, Git, Python 3 and the Go version required by
[`backend/go.mod`](../../backend/go.mod), currently 1.27.1, available on PATH.
The local preview is verified on Apple Silicon macOS; other hosts remain unverified.

```bash
go version
git --version
python3 --version
bash scripts/swarm/demo-offline.sh
```

Run from the checkout root. On first use, Go may download public modules or a
toolchain. “Offline” means no model/provider execution; a disconnected first run
requires those dependencies to be cached already. Once prepared, use
`GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off bash scripts/swarm/demo-offline.sh`
to prevent Go dependency downloads.

The command creates a new temporary output directory and prints its location.
Keep the report and raw logs there for inspection. A failure returns nonzero;
missing/skipped required tests cannot count as a passing demo. Test workspaces
and databases are temporary fixtures; they are not durable user-run artifacts.

For a chosen location, use `bash scripts/swarm/demo-offline.sh --output-dir NEW_DIRECTORY`
with an existing parent directory. Outputs include `report.md`, `report.json`,
`go-test.jsonl`, `go-test.stderr.log` and `command.txt`. Raw logs may contain local
paths; review them before sharing. The [sanitized recorded result](evidence/offline-demo.json)
contains four passes with no skips (8.038 seconds of package execution); it does
not replace running the checks on your checkout.

## What the report means

| Check | Actual execution |
| --- | --- |
| Exact launch and replay | Real planning, SQLite records and launch bridge; fake provider/runtime ports |
| Parallel work and dependency blocking | Two real controlled helper processes in independent copies; no model sessions |
| Cancellation and uncertainty | Controlled runtime outcomes, exact ownership checks and retained reservations |

Passing these checks demonstrates tested coordination behavior. It does not
qualify live model execution, production containment, accepted code, model
quality, token savings or the original SWM-06 milestone.

## If startup fails

- **`go` missing or too old:** install the version in `backend/go.mod` and put it
  on PATH. The optional [full Mac bootstrap](MAC_BASELINE.md) also installs Node
  and desktop tooling; it is unnecessary if Go is already available.
- **Dependency download denied:** populate Go's module cache with network access,
  then repeat with the offline settings above. Do not invent success from a
  compile/download failure.
- **Existing output directory:** choose a new path; the runner preserves earlier
  output rather than overwriting it.
- **Test failure:** inspect the printed report/log paths. Existing wider baseline
  limitations are recorded in [BASELINE_ISSUES.md](BASELINE_ISSUES.md).

Next: [current progress](PROGRESS.md), [launch plan](GITHUB_LAUNCH_PLAN.md), or
[inherited desktop setup](MAC_BASELINE.md). The desktop is AO's existing product
surface; it does not yet expose the Praxis swarm workflow.
