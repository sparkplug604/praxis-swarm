# AI lead workflow reference

For a first project, choose the [Codex walkthrough](GET_STARTED_CODEX.md) or
[terminal walkthrough](GET_STARTED_TERMINAL.md). This page describes the contracts
and operations behind those guides.

The current bridge lets the inherited AO lead prepare a proposal, have the daemon
build and validate execution inputs, submit the two workers, and assemble frozen
outputs for lead review. Fresh orchestrator sessions receive these instructions;
existing sessions need a new turn with the workflow or a fresh session to consume
the updated standing instructions. Use a daemon built from this checkout.

## Project Discovery

The managed lead can persist clarification through native `discovery_get` and
`discovery_save`. Once a project has a discovery record, preparation and new
launches require its current ready `brief` reference (`revision`, `sha256`).
Updates append immutable revisions using `expectedRevision`; older prepared
plans are rejected at new-run creation. See [Project Discovery](PROJECT_DISCOVERY.md).

## Lead workflow

Ask the AO lead to use **Praxis Swarm** for a suitable project goal. The instructions
apply when the user requests that workflow; ordinary AO worker delegation remains
available. The lead must select two independent assignments with disjoint scopes.
Both use the same explicit Codex model and effort. Preserve the user's selections.
If a goal cannot fit this profile, the lead should explain why rather than force a
split. Preparing a proposal does not start model workers.

The lead runs `ao swarm proposal` for a complete example and semantic requirements,
then writes its concrete proposal into session artifacts, outside the project source.
For Codex workspace-write sessions, use the native AO `swarm` tool for daemon
operations: shell networking can remain disabled. The tool is exposed only to an
active project orchestrator and checks the caller project before mutations.
The proposal includes projectId, objective, specification, model, effort, advisory
tokenLimit, tools, and two tasks. Each task supplies its ID/objective/paths/non-goals,
interfaces, exact errors, acceptance checks and progressPath. Each error must be
referenced by both an interface and a check. Check command[0] names a declared tool.
Tools supply name, absolute path and a version label; the daemon derives canonical
paths and executable hashes without running version commands. Version labels remain
supplied metadata. The builder derives sourceCommit and task-scoped manifests from
an independent source capture; it does not invent behavioral requirements.

```bash
ao swarm prepare --proposal /path/to/session-artifacts/proposal.json
```

The shell commands in this guide remain available to users and permissioned
terminals. A sandboxed lead instead calls the native tool:

1. `prepare` with its complete proposal object: returns a compact task summary and preparedId.
2. `start` with preparedId, stable requestId and timeoutSeconds: launches real workers.
3. `get`/`stop` with runId for status/cancellation.
4. `review` with runId to assemble the combined review candidate and check specifications.

Prepared handles last for one native tool host (maximum eight cached proposals).
A restarted tool host must recover an existing run via runId rather than launch
a replacement. The full preparation remains daemon-owned. AO session attribution
is cooperative loopback context, not a new authentication or adversarial boundary.

The daemon resolves the registered project and requires a clean committed source.
It retains source preparation evidence under its configured data directory, checks
context, composes both packets and returns plan/context/preview packets as JSON. Final packets are bound to the actual
run ID when execution starts. Invalid
proposals launch nothing. The lead inspects this response before execution.

```bash
ao swarm prepare --proposal /path/to/session-artifacts/proposal.json \
  --execute --request-id stable-project-request --timeout 240
```

This starts real workers and consumes account usage. The CLI waits for the combined
run report. Retain the run ID printed to stderr and use `ao swarm get ID` or
`ao swarm stop ID`. Interrupting the CLI wait does not stop workers. On an uncertain
submission, retrieve the run; do not use a fresh request ID as an automatic retry.
A regenerated proposal against a changed source may conflict with an earlier
request: retain the exact prepared plan/context and use the original run identity.

## Lead review

```bash
ao swarm review RUN_ID
```

For a completed run this POST operation creates an exclusive review directory from
the retained baseline, overlays each worker's scoped captured changes, verifies
snapshots/candidate identities and tools, and returns directory/checks/instructions
alongside the full run. Each call preserves a separate review preparation. It does
not alter source, worker snapshots, or Git branches. For stopped or uncertain runs,
inspect `get` evidence; review preparation does not convert them into completed runs.

The lead reads both contracts, inspects changes and runs appropriate check argv in
the returned combined directory using its existing session tools and permissions.
The daemon does not automatically execute proposal-supplied commands. Review runs
execute candidate code with the review session's permissions; these worktrees are
not an adversarial sandbox. Save actual command/output/exit evidence in the lead's
session artifacts. Report expected versus actual outcomes, defects and blockers.
A worker's reported checks and a completed run are not independent acceptance.
No automatic merge, publishing or corrective model run is added by this bridge.

Acceptance identity in a generated plan hashes the declared check specification;
it is not a hash of an independently protected evaluator. Context/packet limits
retain the existing supervised boundaries and do not cap native model context or
provider spending. Available usage counters do not establish cost savings.

## API and verification

- POST `/api/v1/swarm/prepare`: proposal -> validated plan/context/packets; no launch.
- POST `/api/v1/swarm/runs`: existing supervised execution.
- POST `/api/v1/swarm/runs/{runId}/review`: create a combined review bundle.

Implementation uses existing AO lead/session tools, daemon project lookup, supplied
plan validation, worktree lifecycle and durable run records. No extra provider SDK,
global model configuration or Personal Praxis RAG integration is introduced.
Offline tests use explicit runtime fixtures. A fresh live demonstration has now
confirmed lead-authored planning, validation feedback, two real concurrent Luna/low
workers and actual lead review checks on one Mac fixture. See
[confirmation evidence](evidence/lead-bridge-confirmation.json) and PROGRESS for
its scope. Broader project quality, recovery and containment remain separate gates.

The existing demo generator also emits `proposal.json` with complete task semantics
and named tools. It can be used with prepare after registering its fixture project.
