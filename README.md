# Praxis Swarm

**Make model handoffs explicit. Run parallel coding agents with results you can review.**

Praxis Swarm gives each worker a task contract, a bounded handoff packet and an
owned file scope, then keeps the execution record and candidate changes together.
Supply a plan, let two agents work in separate worktrees, and review their frozen
outputs before deciding what to accept. It is an independent fork of
[Agent Orchestrator](https://github.com/OrchestratorInc/agent-orchestrator), which
provides the daemon, desktop and agent adapters.

**Supervised source alpha.** The `ao swarm` CLI has completed two concurrent
Luna/low assignments on one Apple Silicon Mac, with independently tested outputs
and observed controller stops. Source is published at
[`sparkplug604/praxis-swarm`](https://github.com/sparkplug604/praxis-swarm).
See the [alpha release](https://github.com/sparkplug604/praxis-swarm/releases/tag/praxis-v0.1.0-alpha.1),
[release checklist](docs/swarm/PUBLICATION.md) and
[current progress](docs/swarm/PROGRESS.md) for validation status.
Build the CLI from source; this release includes no Praxis installer.

## Why use Praxis Swarm?

Turn a prepared plan into parallel work you can inspect, test and approve.

- **Plan with one model, execute with another.** Bring a plan prepared with your
  preferred model, then explicitly choose the worker model and reasoning effort.
  Carry requirements, interfaces and acceptance criteria into each assignment.
- **Give each worker a clear job.** Supply a bounded handoff packet and a defined
  file scope. Start both workers from the same pinned source commit in separate
  worktrees, and flag changes outside their assigned scope.
- **Review exactly what was produced.** Inspect frozen candidate files, hashes
  and patches alongside execution records. Test the outputs before deciding what
  to integrate.
- **Inspect uncertain runs without duplicating work.** Retrieve the existing run
  using the same request ID and identical input. Use explicit stop commands and
  daemon-owned deadlines to manage execution while retaining its evidence.
- **Make informed model choices.** Keep model settings and available usage records
  with each run so you can assess assignments and refine future plans.

Built on Agent Orchestrator's execution foundation, Praxis adds a structured
handoff and evidence workflow for supervised delegation.
[Try the offline demo](#try-it-without-a-model-account) to explore the coordination
workflow without launching models.

## Try it without a model account

You need Bash, Git, Python 3 and Go 1.27.1 on PATH. The documented setup is macOS;
other hosts are not yet qualified:

```bash
git clone https://github.com/sparkplug604/praxis-swarm.git
cd praxis-swarm
bash scripts/swarm/demo-offline.sh
```

From an existing checkout, run just the last command. It prints a new temporary
output directory containing `report.md`, `report.json` and raw test logs. Success
requires all four named tests to pass without skips. Failure returns nonzero and
preserves the logs.

This demo exercises real coordination code, SQLite records and overlapping helper
processes with labelled provider/runtime test doubles. **It launches no models.**
Go may download public modules or a toolchain on first use; a disconnected run
requires a prepared cache. See the [offline quick start](docs/swarm/QUICKSTART.md)
for prerequisites, report interpretation and troubleshooting.

## Run two real workers

The first live profile takes exactly two independent tasks in one clean local
repository, with disjoint file scopes and an explicit Codex model/effort. The
example below creates a disposable duration-parser/formatter project. You also
need Python 3.8+, a working Codex CLI and an existing login with access to the
selected model. Check `codex --version` and `codex login status` first.

From the checkout root, use a fresh shell and an unused loopback port:

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

The generator and daemon startup above do not launch workers. Wait until
`curl --fail "http://127.0.0.1:$AO_PORT/healthz"` succeeds, then register the fixture:

```bash
"$SWARM_DEMO_ROOT/ao" project add --id ghl03-demo \
  --path "$SWARM_DEMO_ROOT/fixture/repository" --worker-agent codex
"$SWARM_DEMO_ROOT/ao" project set-config ghl03-demo \
  --default-branch main --worker-agent codex
```

**The next command starts two real model sessions and consumes account usage.**
Token reservations are advisory, not provider spending caps. The daemon owns the
240-second deadline; an approval request, provider failure or timeout can prevent
completion.

```bash
"$SWARM_DEMO_ROOT/ao" swarm run \
  --plan "$SWARM_DEMO_ROOT/fixture/plan.json" \
  --context "$SWARM_DEMO_ROOT/fixture/context.json" \
  --request-id duration-demo-1 --timeout 240 >"$SWARM_DEMO_ROOT/report.json"
```

The CLI waits and writes one combined JSON report. `completed` means both turns
completed, their controllers were observed stopped, and scoped candidates were
captured with the source preserved. **It does not mean the code is accepted.**
Review and independently test the frozen files before integrating them.
`stopped` may contain partial work; `needs_attention` requires inspection and
returns a nonzero exit status.

Use `ao swarm get RUN_ID` and `ao swarm stop RUN_ID` with the same built binary
and shell environment. Ctrl-C stops the CLI wait, not the workers. Reuse the same
request ID and identical input to recover an uncertain submission. After stops
are confirmed, shut down the scratch daemon with
`curl --fail -X POST "http://127.0.0.1:$AO_PORT/shutdown"` and retain its data for review.
The [full supervised guide](docs/swarm/SUPERVISED_WORKFLOW.md) explains report
paths, recovery, source pinning, shutdown verification and supplying your own plan.

## What the evidence supports

| Capability | Current boundary |
| --- | --- |
| Explicit handoff contracts and context selection | Implemented and tested backend services; the CLI consumes supplied plans |
| Offline scheduling, replay and cancellation | Four required regression tests; helper-process overlap is distinct from model overlap |
| Supplied-plan parallel execution | Two Luna/low workers completed on one Mac, with 12.14 seconds of overlapping provider turns |
| Reviewable outputs | Frozen candidate files, hashes, tracked-file patches, available usage and stop observations |
| Acceptance and integration | Human review and independent testing; no automatic merge or acceptance decision |
| Cost and containment | Advisory budgets; no demonstrated token savings or complete process-containment guarantee |

The [recorded confirmation](docs/swarm/SUPERVISED_WORKFLOW_RESULT.md) includes
independent checks of the combined toy implementation on Python 3.9 and 3.14.
It is evidence for this workflow, not a benchmark of general coding quality.
The separate exact-copy runtime remains gated. Ordinary AO worktrees are not an
adversarial sandbox, and manual AO session restoration is outside this fresh-start
profile. The inherited desktop does not yet expose the Praxis swarm workflow.

## Continue from here

- [Supervised CLI guide](docs/swarm/SUPERVISED_WORKFLOW.md): run, inspect and stop workers; supply your own plan.
- [Planning and handoff contracts](docs/swarm/PLANNING_AND_HANDOFF.md): task ownership, context and packet limits.
- [Release notes](docs/swarm/RELEASE_NOTES.md): this candidate's scope and known limits.
- [Progress](docs/swarm/PROGRESS.md) and [roadmap](docs/swarm/BUILD_PLAN.md): verified work and what remains.
- [Contributing](CONTRIBUTING.md) and [issues](https://github.com/sparkplug604/praxis-swarm/issues): report a problem or propose a focused improvement.

## Upstream and licensing

Built on Agent Orchestrator at commit
`71bb19f5947a64853ee6d11225bc896563878c50`. Original history and license declarations
are retained; see [provenance](docs/swarm/UPSTREAM.md), [LICENSE](LICENSE) and
[third-party declarations](THIRD_PARTY_NOTICES.md). This independent project does
not imply endorsement by OrchestratorInc or OpenAI.

Modification notice: Praxis Swarm replaces the inherited README with the fork's
handoff, offline-demo and supervised-CLI entrypoints. The original is retained in
[the pinned upstream history](https://github.com/OrchestratorInc/agent-orchestrator/blob/71bb19f5947a64853ee6d11225bc896563878c50/README.md).
