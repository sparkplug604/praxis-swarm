# Praxis Swarm upstream provenance

Praxis Swarm is the working name for an independent fork of the public Agent Orchestrator (AO) implementation. It does not imply OpenAI ownership, endorsement, or affiliation. This note records the foundation and setup boundaries; [PROGRESS.md](PROGRESS.md) owns validation results and current work. The full implementation scope is in [BUILD_PLAN.md](BUILD_PLAN.md).

## Pinned source and preserved history

| Item | Recorded value |
| --- | --- |
| Upstream | [OrchestratorInc/agent-orchestrator](https://github.com/OrchestratorInc/agent-orchestrator) |
| Base commit | [`71bb19f5947a64853ee6d11225bc896563878c50`](https://github.com/OrchestratorInc/agent-orchestrator/commit/71bb19f5947a64853ee6d11225bc896563878c50) |
| Local remote | `upstream`, fetch/push URL `https://github.com/OrchestratorInc/agent-orchestrator.git` |
| History at the pin | 3,628 reachable commits including the pinned commit; not a shallow clone |
| Clone mode | Blobless partial clone: `remote.upstream.promisor=true`, `remote.upstream.partialclonefilter=blob:none` |
| Preserved AO archive SHA-256 | `12671035d05d29c86be2a12a17230460d3964373eef3cecce9f716b0f01a7bc1` |

Commit history is retained. GHL-04 fetched all objects reachable from the pinned commit for the publication scan: 77,196 objects, zero missing. The initial blobless configuration remains recorded above; future upstream fetches may again omit blobs. The original downloaded archive, extracted source snapshot, download manifest, and audit findings remain preserved outside this development checkout. The initial checkout comparison matched all 5,416 regular files in the preserved AO snapshot. Development changes belong in this fork rather than those records.

The `upstream` remote remains available for future comparison and updates. No upstream issue, pull request, contribution, push, or publication is part of this baseline setup. Upstream [AGENTS.md](https://github.com/OrchestratorInc/agent-orchestrator/blob/71bb19f5947a64853ee6d11225bc896563878c50/AGENTS.md) and [CONTRIBUTING.md](https://github.com/OrchestratorInc/agent-orchestrator/blob/71bb19f5947a64853ee6d11225bc896563878c50/CONTRIBUTING.md) remain available in the pinned history. Local contributor documentation is modified for this independent fork; any later upstream contribution follows upstream’s process.

## Notices and public boundary

The root [LICENSE](https://github.com/OrchestratorInc/agent-orchestrator/blob/71bb19f5947a64853ee6d11225bc896563878c50/LICENSE) contains Apache License 2.0. The [frontend manifest](https://github.com/OrchestratorInc/agent-orchestrator/blob/71bb19f5947a64853ee6d11225bc896563878c50/frontend/package.json#L8) and [product-ui manifest](https://github.com/OrchestratorInc/agent-orchestrator/blob/71bb19f5947a64853ee6d11225bc896563878c50/packages/product-ui/package.json#L49) declare `MIT`; [cloud-client](https://github.com/OrchestratorInc/agent-orchestrator/blob/71bb19f5947a64853ee6d11225bc896563878c50/packages/cloud-client/package.json#L39) declares `Apache-2.0`. This metadata discrepancy is retained rather than silently relabelled. The source package includes both declared license texts, existing notices and modification notices; see [source-release license treatment](LICENSING.md).

The local foundation is the public Go daemon, Electron/React frontend, public cloud contract, and shared packages. The private control plane is not included. The pinned [.gitmodules](https://github.com/OrchestratorInc/agent-orchestrator/blob/71bb19f5947a64853ee6d11225bc896563878c50/.gitmodules) retains `private/ao-cloud` with `update = none`; do not initialize it for this baseline. The [public-boundary check](https://github.com/OrchestratorInc/agent-orchestrator/blob/71bb19f5947a64853ee6d11225bc896563878c50/scripts/check-public-boundary.sh) checks tracked source references and must run from a real Git checkout. Its result does not prove that runtime startup makes no external requests.

## OpenSwarm selection concepts

SWM-03's new Go context selector adapts the dimension-matching and specificity
ideas in `select_lessons` / `select_patterns` from
[surlymochan/openswarm at b2a70d436ad72f0c5b036730fc86b749ddc68af7](https://github.com/surlymochan/openswarm/blob/b2a70d436ad72f0c5b036730fc86b749ddc68af7/bin/openswarm).
The locally preserved archive has SHA-256
`c65d7215c26bf12d20e130581ccc27e6c9c04be80e8cf42437ed656ebe7d0177`.
That snapshot has an Apache-2.0 root license and no separate NOTICE file. This
is a new implementation of the concepts, not copied Python source; the original
download and license remain preserved. New behavior requires trusted approval,
exact body hashes, normalized scopes and full-packet budgeting with explicit
omissions. See [planning contracts](PLANNING_AND_HANDOFF.md).

## SWM-04 routing and receipt concepts

The new Go routing/accounting implementation adapts OpenSwarm's `shift_effort`,
`clamp_effort`, `worst_window`, `plan_codex`, `route` and atomic budget concepts
from the same pinned archive above. Explicit overrides are validated before
admission; elapsed resets require fresh observations; spare quota never triggers
a premium upgrade. These deliberately differ from permissive override/reset and
quota-driven effort behavior in that source. No upstream Python is copied.

Receipt fields draw on response IDs, model identity, input/output, cache,
reasoning and cost metrics in
[OpenHands software-agent-sdk at 47bbc5e8392c4c75b7f77a1da38bed4a9f53d6a5](https://github.com/OpenHands/software-agent-sdk/tree/47bbc5e8392c4c75b7f77a1da38bed4a9f53d6a5/openhands-sdk/openhands/sdk/llm/utils),
particularly `metrics.py` and `telemetry.py`. Preserved archive SHA-256:
`e0216dd0514ae4d0a389ec359b16b9991f11014ff70493ffdc0b2a27c0cd4833`.
Its root license is MIT, copyright 2026 OpenHands contributors. This is a new Go
implementation of those concepts, with response/operation deduplication and
explicit unknown counters; a missing or failed cost observation never proves
zero billed usage. Original downloaded sources and licenses remain intact.

See [routing and usage](ROUTING_AND_USAGE.md) for the actual local contract and
limitations. These references establish provenance, not live provider support.

## Local baseline prerequisites

The pinned [development guide](https://github.com/OrchestratorInc/agent-orchestrator/blob/71bb19f5947a64853ee6d11225bc896563878c50/docs/development.md#L5) declares Go 1.27.1, Node.js 24, npm 10, and Git. Source terminal sessions on macOS may also require tmux; packaging supplies its own copy. Use checkout-local dependencies, preserve lockfiles, and do not share another checkout's `node_modules`. The frontend resolves product-ui source directly; install `packages/product-ui` dependencies for typechecking. Cloud-client and documentation dependencies are needed for their own suites, not merely to display an empty local board.

Inspect install lifecycle scripts before execution. `npm ci --ignore-scripts --no-audit --no-fund` suppresses dependency install hooks, but Electron and native modules still need explicitly reviewed runtime preparation. The pinned lockfile includes native/install hooks for `better-sqlite3`, `core-js`, `electron-winstaller`, and `fsevents`. Record the actual installation and native-build steps in the baseline evidence.

For a scratch desktop run, set these paths explicitly to new, absolute locations:

| Variable | Purpose |
| --- | --- |
| `AO_DATA_DIR` | Daemon database, cloud-auth store, workspaces, and runtime state |
| `AO_RUN_FILE` | Scratch `running.json`; related settings and app-state marker follow its directory |
| `AO_DEV_ELECTRON_DIR` | Electron development profile, cache, cookies, and local storage |
| `AO_PORT` | An unused loopback port for this run |
| `AO_ACP_RUNTIME_DIR` | Explicit scratch location that suppresses automatic ACP-runtime repair/download; unused during the empty-board baseline |

`AO_DATA_DIR` alone is insufficient: pinned dev code otherwise uses `~/.ao/dev/running.json` and `~/.ao/dev/electron` independently. See [run-file resolution](https://github.com/OrchestratorInc/agent-orchestrator/blob/71bb19f5947a64853ee6d11225bc896563878c50/frontend/src/main.ts#L1242), [Electron profile resolution](https://github.com/OrchestratorInc/agent-orchestrator/blob/71bb19f5947a64853ee6d11225bc896563878c50/frontend/src/main.ts#L297), and [ACP-runtime repair](https://github.com/OrchestratorInc/agent-orchestrator/blob/71bb19f5947a64853ee6d11225bc896563878c50/frontend/src/main.ts#L1975). Keep existing user AO data and credentials untouched. Keep Go/npm/Electron caches and build outputs in the isolated lab as recorded by the local setup procedure.

Set `AO_HARNESS_UPDATE_CHECKS=off`, `AO_TELEMETRY_EVENTS=off`, `AO_TELEMETRY_METRICS=off`, `AO_TELEMETRY_REMOTE=off`, `AO_TELEMETRY_RENDERER=off`, and an empty `AO_SENTRY_DSN` for this baseline. Leave cloud sign-in, remote/mobile connections, and worker/model sessions unused. Outbound denial with loopback allowed provides an additional runtime boundary when available; these flags alone do not guarantee no attempted external requests. Stock startup probes a login-shell environment, GitHub CLI account details, and installed harness readiness. Dev protocol registration may also release an existing Electron `ao-app` handler claim. Record the actual containment and any limitations in [PROGRESS.md](PROGRESS.md).

## Build and download boundary

Use the direct development path after reviewed dependency preparation: `npm run build:daemon -- --dev`, optional local browser-extension compilation, and direct `./node_modules/.bin/electron-forge start` from `frontend`, with the scratch environment above. These are the intended baseline steps; their execution and results are recorded separately. For a standalone daemon, invoke `ao daemon`; pinned `ao start` resolves/downloads/opens the desktop app. Avoid `scripts/daemon-build.sh`, which installs or symlinks a binary into PATH directories.

The [frontend lifecycle scripts](https://github.com/OrchestratorInc/agent-orchestrator/blob/71bb19f5947a64853ee6d11225bc896563878c50/frontend/package.json#L15) attach extra work to `npm run dev`, `build`, `package`, and `make`. Optional browser automation, packaged tmux, cloudflared, ACP adapter/Node runtime, installers, signing, notarization, and publication are excluded from the initial empty-board baseline. In particular, ACP preparation downloads Node 22.23.2 and adapter dependencies, while browser preparation fetches a pinned agent-browser binary and license files. The direct launch plus explicit `AO_ACP_RUNTIME_DIR` bypasses those preparation hooks; it does not qualify the omitted capabilities.

Packaging and full runtime qualification remain explicit work in the [build plan](BUILD_PLAN.md). This document makes no claim that builds, tests, desktop launch, model adapters, or a live swarm have passed. Consult [PROGRESS.md](PROGRESS.md) for evidence and for checks that are failed, partial, skipped, or not run.
