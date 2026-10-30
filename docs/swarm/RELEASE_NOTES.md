# Supervised alpha candidate

Prepared 30 October 2026 for the public
[`sparkplug604/praxis-swarm`](https://github.com/sparkplug604/praxis-swarm) repository.
Publication and release verification are pending; this document does not announce
a published tag, installer or binary. [PUBLICATION.md](PUBLICATION.md) records the
release boundary, and [PROGRESS.md](PROGRESS.md) owns current completion status.

## Included

- A supplied-plan `ao swarm run/get/stop` CLI over the daemon HTTP API.
- Two independent Codex chat workers, explicit model/effort, disjoint file scopes
  and fresh worktrees pinned to the plan's source commit.
- Durable launch intent, idempotent request replay, daemon-owned deadlines and
  visible uncertain launch/stop states without automatic replacement workers.
- Bounded handoff packets, compact supervised instructions that preserve project
  rules, and the existing `accept-edits` permission mapping.
- Frozen candidates, content hashes, tracked-file patches, available usage and
  observed controller state for human review.
- A reproducible offline coordination demo and a disposable live example generator.

## Verified scope

The corrected supplied-plan workflow completed two Luna/low workers on one Apple
Silicon Mac. Their provider-turn intervals overlapped by 12.14 seconds. Independent
checks of the combined frozen toy implementation passed 13 test methods on Python
3.9.6 and 3.14.3, including 1,010 exactness/roundtrip cases. Source preservation,
candidate scope, controller stops and scratch-daemon shutdown were checked.
The [recorded confirmation](SUPERVISED_WORKFLOW_RESULT.md) retains the earlier
partial diagnostic separately.

The offline demo requires four named tests to pass without skips. Its controlled
helper-process overlap is distinct from live model overlap. Neither evidence set
establishes general coding quality or qualification on other machines.

## Limits

Token reservations are advisory, not provider-enforced spending caps. Packet
bounds do not bound all native model context, and recorded usage does not establish
billing cost or token savings. Model availability depends on the user's account.

A `completed` run means the supervised execution and capture checks succeeded;
acceptance and integration still require human review and independent testing.
There is no automatic acceptance decision, branch merge, model retry or planner
call in this profile. The supplied plan must contain exactly two independent tasks.

Only one Mac setup has live confirmation. Ordinary AO worktrees are not adversarial
containment, and observed controller stops do not prove complete descendant-process
absence. The separate exact-copy runtime remains gated. Restart reconciliation
can require attention; manual AO session restoration is outside this fresh-start
profile. The inherited desktop does not expose the Praxis swarm workflow yet.

## Start and contribute

Use the [offline quick start](QUICKSTART.md) first, then the
[supervised CLI guide](SUPERVISED_WORKFLOW.md) for explicit live execution.
See [CONTRIBUTING.md](../../CONTRIBUTING.md) for focused fixes and evidence-based
reports. No model account or paid usage is needed for the offline demo.

Praxis Swarm is an independent modification of Agent Orchestrator. Original source
history and attribution are retained. See [UPSTREAM.md](UPSTREAM.md),
[LICENSE](../../LICENSE) and [THIRD_PARTY_NOTICES.md](../../THIRD_PARTY_NOTICES.md).
