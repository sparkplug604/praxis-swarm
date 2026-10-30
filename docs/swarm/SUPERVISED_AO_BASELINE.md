# Two-worker inherited AO baseline

GHL-02 demonstrated two live Luna/low coding turns on Apple Silicon macOS on
29 October 2026. They used the inherited AO daemon/API, separate worktrees and
the existing ChatGPT login. This is execution evidence for the supervised AO
profile. Praxis plan-to-launch integration and SWM-06 remain incomplete.

## Recorded result

The [sanitized result](evidence/supervised-ao-baseline.json) records both session
and provider-turn identities, requested and native-observed model/effort,
candidate hashes, overlap, test outcomes, permissions, usage and stop observations.
Raw native transcripts contain personal context and are not distributed.

- Two independent duration-helper implementations; 11 seconds of overlapping
  provider-backed turns, measured at one-second resolution. No speedup claim.
- 10 test methods passed on the frozen combined source under Python 3.9.6.
  Additional review reproduced a Python 3.14.3 failure for 5,001-digit inputs.
  The fixture's no-maximum-magnitude contract is therefore not fully portable.
  Candidate code was preserved; no correction run or production integration.
- Both agents stopped through `exit-agent` with worktrees retained. Independent
  controller reads and graceful daemon shutdown were checked. These observations
  do not prove containment of arbitrary detached descendants.
- Worker totals: 238,274 input tokens, of which 204,032 were cached, plus 2,132
  output. Cached input is included in input totals. These counters exclude the
  coordinator/reviewer and do not establish billing cost or token savings.

Each worker received 15,718 bytes of AO system instructions. Native developer
messages included further memory/skill context, reaching 47,022 characters; base
model instructions added 18,069 characters. Repeated completions reuse that
context. GHL-03 should reduce and measure irrelevant context using supported
configuration without weakening task contracts or execution permissions.

## Existing API flow

These are the verified API operations, not a finished Praxis command. Run an
isolated daemon with unique `AO_DATA_DIR`, `AO_RUN_FILE` and loopback `AO_PORT`.
Keep telemetry/update settings scoped to that process. Use a clean committed
throwaway repository with a local `main` branch and two disjoint assignments.
Do not point the trial at an active project or the user's normal AO data.

1. `POST /api/v1/projects` with `path`, `projectId`, `name` and
   `config: {"defaultBranch":"main","worker":{"agent":"codex"}}`.
2. `POST /api/v1/sessions` twice, with unique `clientRequestId`, `branch` and
   `displayName`, plus `projectId`, `kind:"worker"`, `harness:"codex"`,
   `mode:"chat"`, `model:"gpt-5.6-luna"`, `effort:"low"`,
   `approvalMode:"accept-edits"` and the bounded `prompt`.
3. Retain `/api/v1/sessions/{id}/conversation` snapshots and
   `/api/v1/desktop/sessions/{id}/workspace` results. Intersect actual turn
   intervals; two session rows alone do not demonstrate simultaneous work.
4. `POST /api/v1/sessions/{id}/exit-agent` with `{}`. Check the response and
   independently confirm the controller is stopped. Preserve patches, files and
   hashes; run independent checks against frozen source. Do not use `kill` or
   cleanup as a substitute: those paths can destroy worktrees.
5. Shut down the owned scratch daemon through `POST /shutdown` and verify its
   process/listener/run file are gone. Retain uncertainty when observations fail.

Explicit `accept-edits` maps to on-request approvals and workspace-write. In this
upstream version, explicitly requesting `default` maps to unrestricted execution;
do not substitute it. Explicit branch names suppress optional task-naming calls.
No automatic model retry, substitution or permission bypass belongs in this demo.

The installed PATH wrapper was unusable on the test Mac. An existing bundled
Codex 0.153.4 binary was selected through this daemon's PATH only; the login status
reported ChatGPT authentication. Other installations require their own readiness
check. No global settings or credentials were rewritten.

The local diagnostic driver used for this run has incomplete launch/stop error
recovery and is not published as a general runner. GHL-03 must handle those cases
through existing daemon/API/CLI boundaries before offering one supported workflow.
See [PROGRESS](PROGRESS.md) for the next task and [the launch plan](GITHUB_LAUNCH_PLAN.md)
for acceptance and publication gates.
