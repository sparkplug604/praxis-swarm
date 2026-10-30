# Run a supervised two-worker swarm

The `supervised-ao-v1` profile connects a supplied plan to ordinary AO Codex chat
sessions. It creates two worktrees, records launch intent before dispatch, stops
the controllers after their turns, and retains candidate files, hashes and
tracked-file patches for human review. It does not merge changes or execute an
independent acceptance test automatically. The [corrected live confirmation](SUPERVISED_WORKFLOW_RESULT.md)
completed both workers on one Mac; the coordinator independently tested the
frozen code afterward. Publication and broader runtime qualification remain open.

## Prerequisites and first run

Use the Mac setup described in [the quick start](QUICKSTART.md), Git, Python 3.8+,
Go 1.27.1, and a working Codex CLI with an existing login. Verify `codex --version`
and `codex login status` yourself. No API key or paid account is created by this
workflow. Model availability and usage limits depend on your account.

Build the CLI/daemon from the repository root:

```bash
SWARM_DEMO_ROOT="$(mktemp -d /tmp/praxis-demo.XXXXXX)"
go -C backend build -o "$SWARM_DEMO_ROOT/ao" ./cmd/ao
export AO_DATA_DIR="$SWARM_DEMO_ROOT/data"
export AO_RUN_FILE="$SWARM_DEMO_ROOT/daemon.json"
export AO_PORT=43220
export AO_HARNESS_UPDATE_CHECKS=off
export AO_TELEMETRY_EVENTS=off AO_TELEMETRY_METRICS=off AO_TELEMETRY_REMOTE=off
python3 scripts/swarm/create-supervised-demo.py \
  --output-dir "$SWARM_DEMO_ROOT/fixture" \
  --model gpt-5.6-luna --effort low
"$SWARM_DEMO_ROOT/ao" daemon >"$SWARM_DEMO_ROOT/daemon.log" 2>&1 &
```

Choose a free port and a fresh shell without unrelated `AO_*` overrides. Keep
these variables in that shell. The generator refuses an existing output path;
it commits a disposable fixture and writes the plan and matching context bodies.
It launches no models. The source repository deliberately contains unfinished
helpers: their tests fail until the workers implement them.

Wait until `curl --fail "http://127.0.0.1:$AO_PORT/healthz"` succeeds, then:

```bash
"$SWARM_DEMO_ROOT/ao" project add --id ghl03-demo \
  --path "$SWARM_DEMO_ROOT/fixture/repository" --worker-agent codex
"$SWARM_DEMO_ROOT/ao" project set-config ghl03-demo \
  --default-branch main --worker-agent codex
"$SWARM_DEMO_ROOT/ao" swarm run \
  --plan "$SWARM_DEMO_ROOT/fixture/plan.json" \
  --context "$SWARM_DEMO_ROOT/fixture/context.json" \
  --request-id duration-demo-1 --timeout 240 >"$SWARM_DEMO_ROOT/report.json"
```

The final command starts **two real model sessions** using the plan's explicit
model and reasoning effort. Their token reservations are advisory, not a provider
spending cap. Permissions use AO's `accept-edits` mapping: on-request approvals
and workspace-write. An approval, provider failure or timeout can prevent completion.
No automatic retry, substitute model, branch merge or corrective model run occurs.

The daemon must be the newly built binary; an older daemon will not expose these
routes. This first profile requires exactly two independent worker tasks in one
active local repository, one explicit Codex model/effort, disjoint path scopes,
and a clean source checkout matching the plan commit. AO creates fresh branches
from that pinned commit and verifies the result before the provider starts.
Existing branch/workspace collisions are rejected; no default-branch fallback
is used for this profile.

## Read the report and recover

While waiting on a running submission, the CLI prints its ID on stderr; stdout
remains JSON. For immediate or completed responses, read the ID from JSON:

```bash
"$SWARM_DEMO_ROOT/ao" swarm get RUN_ID
"$SWARM_DEMO_ROOT/ao" swarm stop RUN_ID
```

- `running`: the daemon is supervising the run. `--wait=false` returns immediately.
- `completed`: both provider turns completed, controllers were observed stopped,
  source preservation passed, and scoped candidates were captured. **This is not
  acceptance of their code.** Independently inspect and test the frozen files.
- `stopped`: execution ended without two successful turns, but controller stops
  were confirmed. This includes cancellation/deadline or provider/observation
  failures; inspect each task observation/error. Candidates may be incomplete.
- `needs_attention`: launch identity, stop, capture, scope or source preservation
  could not be established. Inspect each task's status/error. The CLI exits nonzero.

Ctrl-C stops the CLI wait, not the workers. The daemon deadline still applies;
use `swarm stop RUN_ID` to cancel explicitly. If the initial HTTP reply is lost,
submit the **same request ID and identical input** to retrieve the recorded run.
Changed input under that ID is rejected. A new ID authorizes a new run; do not
use one merely to retry an uncertain launch. After daemon restart, an unfinished
record becomes `needs_attention`; explicit stop reconciles recorded session IDs
or request IDs without launching replacements. Manual restoration through
ordinary AO session restore is outside this fresh-spawn profile; it recomputes
ordinary AO instructions rather than preserving the compact profile.

`snapshotPath` points to a JSON snapshot; its sibling `files/` directory contains
the frozen candidate. `candidateHash` covers paths, content, sizes and executable
bits. Snapshot records retain fuller filesystem metadata. `diffPath` is a binary
Git patch for tracked files; untracked files are present in the snapshot, not
necessarily in that patch. Changed files outside the assigned path scope are
retained but mark the run `needs_attention`. Worktrees and artifacts are preserved.

Observations include configured model/effort, available conversation usage and
actual launch prompt byte counts. `modelEvidence` distinguishes settings from
independent native telemetry. `promptEvidence` marks missing counts after restart
or replay. Packet limits exclude inherited AO instructions and native memory,
skills and tool messages; this workflow does not prove reduced cost or savings.

When both controllers are stopped and evidence is retained, shut down this
scratch daemon with `curl --fail -X POST "http://127.0.0.1:$AO_PORT/shutdown"`.
Check its health endpoint is no longer reachable and its run file is gone. Keep
the data directory for review. Do not use a worktree-deleting cleanup command as
a substitute for stopping agents.

## Supply your own plan

The generated `plan.json` and `context.json` are a complete example. Contracts use
the existing [plan/handoff schema](PLANNING_AND_HANDOFF.md). Each context body
must match its reference's SHA-256. Spec/source-manifest bodies are supplied data,
not Praxis approval receipts. Tool paths must be canonical and match their recorded hashes on the daemon
host. Version is supplied metadata bound to that hash; the fixture generator
records it using `--version`, while the service checks the path and bytes. The actual source commit and snapshot are checked
separately. No planner-model call is added.

Each CLI input file is limited to 2 MiB; the service bounds the combined request
to 4 MiB. Each rendered worker prompt is at most 16 KiB and must also fit its
declared envelope bound. Context requiring lesson/pattern approval is unsupported
in this profile. The existing strict scheduler, quota admission and exact-copy
qualification remain separate; this workflow neither creates their receipts nor
removes their gates. Native worktrees are not adversarial containment.

API equivalents are `POST /api/v1/swarm/runs`, `GET /api/v1/swarm/runs/{runId}` and
`POST /api/v1/swarm/runs/{runId}/stop`. The create body contains `requestId`, `plan`,
`context` (an array of `{id,body}`), and `timeoutSeconds` (10–600). Creation/replay
returns HTTP 202; get/stop returns 200. Responses use the normal AO response
envelope. The CLI owns only transport/polling; the daemon owns deadlines, durable
records and session lifecycle.

See [PROGRESS](PROGRESS.md) for verified evidence and publication status.
