# Praxis Swarm build progress

This is the single progress authority for this fork. Read it before selecting work.
The [GitHub launch plan](GITHUB_LAUNCH_PLAN.md) defines the current first-release
sequence; the [full build plan](BUILD_PLAN.md) retains the wider requirements.
Older game/Praxis handoffs and experimental boards do not override this record.

## Project Discovery — 30 October 2026

Implemented persistent, versioned project briefs and clarification questions through
native lead tools, terminal commands and HTTP. Readiness is derived from supplied
fields and required answers. Plans bind to the current ready revision/hash;
incomplete or stale discovery blocks preparation/new runs. Immutable history and
optimistic concurrency preserve prior decisions. Legacy supplied-plan projects
remain compatible; existing run replay remains available after brief changes.
[Guide](PROJECT_DISCOVERY.md).

Full affected domain, planning, supervised, storage, CLI, prompt, HTTP and telemetry
tests passed. Discovery/MCP/lead race checks passed; backend lint reports zero issues,
frontend type checking passed, generated API output reproduces exactly, and six
Python tests plus public-boundary checks pass. Initial sandbox-only loopback
failures were rerun with the required local test-server permission.

The live fixture asked questions with no workers, retained revision one through a
daemon restart, resumed the same native lead, incorporated scripted answers in
ready revision two, and completed two-worker execution/review. Eight actual lead
checks and 11-test discovery passed. Independently reconstructed candidates pass
14 tests on Python 3.9.6 and 3.14.3. Launch config explicitly selected Standard.
An initial masked-error/question-schema defect was fixed with regression coverage;
a harness --steer omission was repaired before resuming the same lead. Failed
checks are retained and included in the local usage receipt.
[Confirmation evidence](evidence/project-discovery-confirmation.md).

Implementation tokens, failed/live actors, cached input and account observations
are recorded outside the repository for this build. The initial processed-token
forecast underestimated the repeated input from this long implementation chat.
Current-chat speed was not machine-verifiable; the user reported selecting Default.
Credit observations are account-wide and are not isolated invoices. No paid model
calls were added to CI. Broader discovery-quality and project-scale trials remain
future work.

## Terminal parser correction and rerun — 30 October 2026

Corrected the captured parser in a separate evidence artifact by moving its
field-length validation before separator indexing. The added truncated-prefix
regression fails on the original parser and passes on the correction on both
Python runtimes. New generated fixtures include this regression (11 tests);
the generator's six local tests pass. Original failure records, snapshots and
historical test suites remain unchanged.

A fresh terminal trial used one Sol/high lead and two Luna/low workers: nine
actual declared checks and 11 discovery tests passed; lead review accepted the
candidate. Independent testing passed all 14 tests on Python 3.9.6 and 3.14.3.
Native settings, scoped captures, source preservation and daemon cleanup were
verified. The prepublication clone used the local candidate commit; the rest of
the terminal workflow was reused. No replacement run, substitution or merge.
[Correction, reproduction and rerun evidence](evidence/terminal-parser-rerun.md).
Broader project qualification remains open.

## First-project walkthroughs — 30 October 2026

Added [Codex](GET_STARTED_CODEX.md) and [terminal](GET_STARTED_TERMINAL.md)
walkthroughs, linked from the README and the lead-workflow landing page. The
previous technical guide is preserved as [LEAD_REFERENCE.md](LEAD_REFERENCE.md).
Both paths were executed from fresh public clones using separately isolated data,
Sol/high leads and two Luna/low workers each. Literal terminal command blocks,
actual lead checks, six distinct native provider sessions, scoped captures,
source preservation and owned-daemon cleanup were verified. The Codex setup was
performed by the existing desktop chat consuming the request; no blind new-chat
usability result is claimed.

Both candidates passed 10 supplied tests and the unchanged 13-test independent
suite on Python 3.9.6/3.14.3. The terminal lead additionally caught a truncated-input
parser defect and marked the candidate **changes required**; independent
reproduction confirmed it on both runtimes. The Codex-assisted candidate passed
review and that supplemental check. Candidates were preserved without correction,
replacement workers or merge. The guides now explain this review outcome.
[Verification details and sanitized evidence](evidence/walkthrough-verification.md).

All eight hosted workflows for implementation commit `f13fe4385` have now completed
successfully, including the formerly pending Go, Frontend and Supervised alpha
jobs. This documentation change leaves implementation and the existing alpha tag
unchanged. Further fixture work would scope a correction to the retained parser
candidate; broader task coverage remains open.

## Lead workflow publication — 30 October 2026

Published the reviewed README, lead proposal/builder, execution and review bridge,
native stdio permission fix, generated API client, tests and sanitized evidence for
`sparkplug604/praxis-swarm`. Selected frozen Sol/Luna demonstration files and the
unchanged independent evaluator are included for a model-free reproduction check.
Raw transcripts, credentials and runtime/cache state remain outside the repository.
Prepublication checks passed: full planning/supervised race suites, focused bridge
race tests, full HTTP/API tests, reproducible API generation, frontend type checking,
six Python tests, public boundary and zero lint issues. The selected captured demo
passes all 13 tests on both Python versions. Staged changes have zero secret-scanner
findings and checked local links. Full inherited suites are left to hosted CI.
The existing alpha tag remains unchanged; this publishes source on main, not a
new tag or installer. Implementation commit:
[`f13fe4385`](https://github.com/sparkplug604/praxis-swarm/commit/f13fe43856652995a418b273ed91670f9119f38b).
A fresh clone from GitHub builds the CLI and passes all 13 captured-demo checks,
with a clean checkout. Hosted cross-platform CLI E2E, secret scanning, public
boundary, mobile and workflow-gate checks passed. At this publication update,
[supervised alpha](https://github.com/sparkplug604/praxis-swarm/actions/runs/41160447117),
[full Go](https://github.com/sparkplug604/praxis-swarm/actions/runs/41160447104), and
[frontend](https://github.com/sparkplug604/praxis-swarm/actions/runs/41160447024)
workflows are still running and are not claimed as passed. This metadata-only
update intentionally skips CI; implementation is identical to the linked tested
commit. Next verification: inspect those remaining hosted outcomes.

## Sol/high lead with Luna/low workers — 30 October 2026

**Pass on the same disposable Mac fixture.** The user selected Sol/high as lead
and Luna/low as both workers. The fixture file hashes and AO binary matched the
prior all-Luna trial. Sol created its own proposal from the contract/tests, passed
preparation on its first submission, launched exactly two workers through the
native stdio tool, inspected the combined code and executed eight declared checks
plus full unittest discovery. Tool invocation/cwd/output/exit evidence independently
supports the reported review. No prewritten proposal was supplied to the lead.

Native telemetry verifies **gpt-6.1-sol/high** for the lead and **gpt-5.6-luna/low**
for both workers; all retained network-disabled workspace-write sandboxes.
Worker provider turns overlapped **17.841664 seconds**. Captured candidate hashes,
file scopes, unchanged source/tests and observed stopped controllers were checked.
The unchanged independent oracle and frozen suites passed **13 tests on Python
3.9.6 and 3.14.3**. The owned daemon exited 0 and its health endpoint/run file were
absent. Existing implementation and game work were preserved before this update.

Catalog preflight initially treated an empty manually selected model list as
unconfirmed. It created zero sessions; AO permits direct model selection, so the
trial then submitted the exact selected IDs and verified actual native telemetry.
No model substitution, replacement worker, production merge or publication.

[Sanitized evidence](evidence/lead-sol-luna-confirmation.json). Raw private local
archive: `outputs/swarm-build/lead-sol-luna-20261030`, actual execution under
`execution/`. Per-session counters and prior-run comparison are retained; cached
input is included in input totals. These runs establish this mixed-model workflow,
not measured billing savings or general model superiority. Astra was not tested.

**Next gate:** a separately authorized real-project task and broader recovery/
review evaluation. The publication entry above records repository status.

## Lead-to-swarm live confirmation — 30 October 2026

**Pass on one disposable Mac fixture.** A fresh Luna/low AO lead created its own
proposal from the duration contract and frozen tests, corrected two schema/semantic
validation errors, prepared the plan, and launched exactly two Luna/low workers
through the native AO stdio swarm tool. Provider turns overlapped **12.065120s**.
Both workers completed; their scoped candidate files and hashes were verified.

The lead assembled a combined review directory, inspected both implementations,
and actually ran the formatter suite, parser suite and full discovery. Independent
inspection of tool commands/cwd/exit results confirms execution coverage for all
four declared check IDs (pairs share the same module-suite argv). The lead saved
proposal.json, review-report.json and check-results.log in its session artifacts.
An unchanged independent oracle plus frozen suites passed **13 tests on Python
3.9.6 and 3.14.3**, including 1,010 oracle/roundtrip cases.

The permission fix adds a project-scoped `swarm` tool to AO's existing native
stdio server. It supports prepare/start/get/stop/review for an active orchestrator's
project. Worker-role calls and cross-project mutations are rejected; prepared handles
are private to one tool host and capped. Compact responses omit duplicated packets.
The lead uses this supported tool transport rather than sandboxed shell loopback.
No general model network grant or sandbox bypass was added. Native telemetry
confirms all three sessions used `gpt-5.6-luna`, low, workspace-write, network off.

Focused MCP/lead race regressions pass and final lint reports zero issues. Existing
source/fixture hashes and game status were preserved before this evidence update;
the owned daemon exited 0 and its health endpoint/run file disappeared. Both
workers were already observed stopped; redundant exit probes returned 409 and
are retained as such. The first independent audit expected a post-probe snapshot;
it was corrected to use the independent pre-probe stopped snapshot, without
changing candidates/oracle. Prior blocked trial evidence remains intact.

[Sanitized result](evidence/lead-bridge-confirmation.json). Raw private local archive:
`outputs/swarm-build/lead-live-mcp-20261030`. Native usage is recorded per session;
there is no comparative cost/savings claim. No production merge, publication,
model substitution or replacement-worker run occurred.

**Next gate:** try a separately authorized real-project task and strengthen review
coverage/report validation and tool-host recovery. This confirmation establishes
one complete lead-to-workers-to-review path, not general autonomous coding quality
or adversarial containment. The publication entry above records repository status.

## Lead live diagnostic — 30 October 2026

**Result: blocked before worker execution.** One actual Luna/low AO lead read the
frozen duration specification/tests, created and corrected its own two-task
proposal in session artifacts, and stopped when its workspace-write sandbox
rejected loopback access for `ao swarm prepare`. No workers launched and no lead
review occurred. This is not an end-to-end pass.

The coordinator subsequently submitted the unchanged lead-authored proposal for
preparation only: full daemon validation passed. Native telemetry independently
confirms `gpt-5.6-luna`, low effort, on-request/native auto_review, workspace-write
and network disabled. Lead counters: 662,726 input (634,624 cached), 5,715 output,
668,441 total; counters are not a billing estimate or savings result.

An earlier setup request was rejected before any session because it supplied a
worker-only request key on lead creation. Both that preflight and the actual
trial are preserved. The actual trial had one lead, zero workers, no replacement
models, no implementation fallback, and no permission bypass. Coordinator-only
proposal validation is recorded separately from what the lead accomplished.

Both owned daemon executions exited 0, their run files were removed, and the
final health endpoint was absent. Implementation and fixture hashes and game
status were unchanged before this evidence update. Local archive:
`outputs/swarm-build/lead-live-20261030`, with `result.json` as the compact result.
Raw native conversations and authentication state remain private and untracked.

**Next gate:** connect the lead to its owned daemon through a supported,
permission-aware mechanism while preserving worker sandbox boundaries, then
perform a fresh live trial. Do not fix this by broadly disabling sandboxing or
by having the coordinator launch workers on the lead's behalf.

## Lead connection implementation — 30 October 2026

Added `ao swarm proposal`, daemon-owned proposal preparation, optional execution
through the existing run API, combined frozen-candidate review preparation, and
AO orchestrator instructions for the full workflow. See [lead guide](LEAD_WORKFLOW.md).
The builder derives source/context/tool hashes and preserves the lead's explicit
model and behavioral decisions. Strict parsing and the existing supervised
validator reject malformed, overlapping or incomplete proposals before launch.

Review preparation creates a fresh combined directory and returns check argv and
contracts; the lead performs actual checks through its session tools and retains
its report. The daemon does not itself execute the check commands or record a
verified acceptance verdict. Existing source and frozen candidates are preserved.

**Validation:** full planning and supervised-service race suites passed, plus
focused CLI/API/lead-instruction regressions and registered-command telemetry.
Offline fixtures cover preparation, two fixture worker launches, request replay,
combined candidates and tamper rejection. API/client regeneration, frontend type
checking and six Python tests passed; final lint reports zero issues. An existing
one-second parent-deadline fixture expired before both Git worktrees started under
race instrumentation; its startup allowance is now five seconds, retaining all
deadline/stop assertions. Earlier failed logs are preserved. No live model calls
were used. Receipts are in `outputs/swarm-build/lead-*`.
**Next gate:** run a fresh live lead-driven demonstration with explicitly selected
lead/worker models and record actual review evidence. Do not count offline fixtures
or prompt instructions as proof of live autonomous planning/review.

## Active launch work — 29 October 2026

**GHL-01: candidate implementation and offline example complete.** The public
destination is `sparkplug604/praxis-swarm`. **GHL-02: two live AO workers demonstrated.**
**GHL-03: supplied-plan supervised CLI integration confirmed.** A fresh authorized
trial of the reporting fix completed both Luna/low workers, passed frozen-code
acceptance and verified clean shutdown. The earlier partial trial remains intact.
**GHL-04: source-alpha release package complete.** Public repository:
[sparkplug604/praxis-swarm](https://github.com/sparkplug604/praxis-swarm).
Release: [praxis-v0.1.0-alpha.1](https://github.com/sparkplug604/praxis-swarm/releases/tag/praxis-v0.1.0-alpha.1). The corrected implementation `5d780a411`
passed the hosted supervised-alpha gate, CLI E2E on Mac/Linux/Windows/container,
public-boundary and secret checks. Local Mac/Linux lint reports zero issues;
clean-checkout build/offline demo and affected race suites passed. Full inherited
Go and Frontend workflows are still running at publication and are **not claimed
as passed**. No additional model trials were used. Existing game work is preserved.

[Release notes](RELEASE_NOTES.md) and [sanitized evidence](evidence/source-alpha-release.json)
record validation, corrections, limits and CI links. Local detailed receipts are
in `outputs/swarm-build/ghl-04`. The release metadata changes documentation only;
its implementation is identical to the tested commit above.

**Next gate:** inspect the two outstanding hosted workflows and address any actual
failures before promoting beyond this alpha. Full SWM-06 runtime qualification,
automated acceptance/integration and installers remain future work.

Publication may proceed as a clearly labelled developer preview before live
integration. The supervised alpha requires the actual GHL-02/03 parallel model
demo; it does not satisfy the full original SWM-06 contract. No model launch,
GitHub destination selection, commit or publication occurred during GHL-01.

## GHL-03 fresh confirmation — 29 October 2026

- The user authorized a new trial. All 175 previously recorded source files and
  the corrected binary matched their saved hashes. New evidence, fixture, project,
  request and session identities were used; the earlier trial was not overwritten.
- The actual `ao swarm run` command completed **two Luna/low workers**, with
  **12.141718 seconds of overlapping AO provider-turn intervals** (12.100 seconds
  in native task events). Native telemetry independently confirms model/effort,
  on-request approvals, workspace-write and disabled network access.
- The frozen combined implementation passed **13 test methods on Python 3.9.6
  and 3.14.3**, including 1,010 exactness/roundtrip cases and huge-input/type-error
  rejection. Independent code review found no concrete defect. Source commit,
  source files, candidate hashes and per-worker file scope were independently checked.
- Both controllers were independently observed stopped. The owned daemon exited
  0; its health endpoint and run file were absent. No reporting escalation,
  approval proxy, replacement worker, production integration or automatic retry.
- Each AO system prompt was **1,289 bytes**, down from 15,821 in the partial trial;
  native developer messages were 32,615 characters instead of 47,130. Worker
  counters were **263,032 input** (216,960 cached), **3,075 output**, **266,107 total**.
  These exclude coordinator/review usage. This is not a controlled savings or
  billing benchmark; the earlier trial had a different completion outcome.
- The local evaluator initially hit Python 3.9's ISO parser limitation on a valid
  five-digit timestamp, before tests executed. The unchanged evaluator completed
  under Python 3.14; candidate tests still ran on both runtimes. Frozen candidates
  and the predeclared oracle were unchanged, and the partial assembly was retained.

[Confirmation evidence](evidence/supervised-workflow-confirmation.json) and
[trial findings](SUPERVISED_WORKFLOW_RESULT.md) distinguish this pass from the
retained earlier diagnostic. Local archive: `outputs/swarm-build/ghl-03-confirmation`.
The minimal supervised launch gate is complete. Full SWM-06 containment/qualification,
automatic acceptance/integration and GHL-04 publication remain open.

## GHL-03 connected workflow — 29 October 2026

- Added `ao swarm run/get/stop` through HTTP/service boundaries, existing SQLite
  storage, strict two-task supplied-plan validation, bounded packets and an
  exclusive fixture generator. AO owns worktrees and live controllers. Every
  launch intent is durable before dispatch; replay/restart never relaunches it.
- AO now accepts an optional pinned source commit for this profile, creates only
  fresh branches and verifies HEAD before provider launch. Candidate snapshots,
  content hashes, tracked-file patches and scope/source checks preserve reviewable
  outputs. Unknown launch/stop states remain visible; no automatic retries.
- The actual CLI launched **two Luna/low sessions**, with **19.408745 seconds of
  overlapping provider-turn intervals**. Native telemetry confirmed model,
  effort, on-request approvals and workspace-write. Source files stayed intact.
- **Partial result, not live qualification:** formatter completed; parser wrote
  its helper and passed its own tests but followed inherited AO instructions to
  call `ao report --done`. The loopback call requested sandbox escalation and
  awaited approval. The 240-second deadline stopped it; the recorded parser turn
  is failed and the overall run is `stopped`. No approval proxy or model retry.
- Both frozen candidates passed **13 independent test methods on Python 3.9.6
  and 3.14.3**, including 1,010 exactness/roundtrip cases and huge-input rejection.
  Both controllers were independently observed stopped; the owned daemon exited
  0 and its listener/run file disappeared. Worktrees/candidates remain retained.
- Worker counters: **359,454 input**, including **304,768 cached**, **3,340 output**,
  **362,794 total**. These exclude coordinator/review usage and prove no savings.
  The tested prompts carried 15,821 AO system bytes per worker plus native context.
- Corrected fresh supervised starts to use compact assignment instructions and
  the native final response, retaining project rules and permission boundaries.
  At that diagnostic stopping point the change was regression-tested only.
  The fresh confirmation above now verifies consumption and completion; savings
  remain unproven. Ordinary AO
  workers and global Codex settings are unchanged; manual AO restore is outside
  the corrected spawn-only profile.
- Affected service, planning, storage, API/CLI, daemon, source-pinning and schema
  checks passed, including race coverage for replay, cancellation, workspace
  overlap and a status-read race. Frontend type checking and the public-boundary
  check passed. Full release checks remain GHL-04; prior baseline issues persist.

See [CLI instructions](SUPERVISED_WORKFLOW.md), [trial findings](SUPERVISED_WORKFLOW_RESULT.md)
and [sanitized evidence](evidence/supervised-workflow-trial.json). Local raw evidence,
prior source snapshots, tested binary and the pre-correction source are retained
in `outputs/swarm-build/ghl-03` (not distributed). No game changes, integration,
publication, new billing route or global model/configuration changes occurred.

## GHL-02 supervised AO baseline — 29 October 2026

- The user selected **Luna with low reasoning**. Two real Codex chat sessions
  ran through the inherited AO HTTP API on a disposable committed repository,
  using the existing ChatGPT login and distinct AO worktrees. Native telemetry
  confirms `gpt-5.6-luna`, `low`, on-request approvals, workspace-write and
  network disabled. Explicit branch names suppressed optional naming calls.
- Both provider-backed turns completed, with **11 seconds of overlap** in AO's
  second-resolution timestamps. Parsing and formatting assignments changed only
  their assigned files. Source checkout and protected fixture files stayed intact.
- The frozen combined submission passed **10 unittest methods** on Python 3.9.6,
  including independent exactness and roundtrip checks. Review found a separate
  portability defect: both helpers reject 5,001-digit values on Python 3.14.3's
  default conversion limit, contrary to the fixture's unrestricted magnitude
  contract. Reproduced; frozen code preserved; no corrective model run or merge.
- Both `exit-agent` calls returned HTTP 200; independent reads confirmed stopped
  controllers with worktrees retained. The scratch daemon shut down gracefully
  (exit 0); its observed PID, listener and run file were absent afterward. This
  does not qualify arbitrary detached descendants or adversarial isolation.
- Worker usage: 238,274 input tokens, including 204,032 cached; 2,132 output;
  240,406 total. These are provider-reported conversation counters, excluding
  coordinator/reviewer usage. Billing and comparative savings are unknown. Large
  inherited instructions and native memory/skill context warrant measurement and
  reduction in GHL-03, rather than assuming lower-model dispatch is efficient.
- The PATH Codex wrapper was broken. A working existing app-bundled binary
  (0.153.4) was selected only through the scratch daemon's PATH; no installation,
  global configuration change, credential copying or new billing route occurred.

See the [baseline and reproduction notes](SUPERVISED_AO_BASELINE.md) and
[sanitized result](evidence/supervised-ao-baseline.json). Raw evidence, frozen
files, patches, review and preservation receipts remain in local archive
`outputs/swarm-build/ghl-02` (not distributed). No production code was changed.
The local HTTP diagnostic is not a supported user entrypoint; its error recovery
is incomplete. Both stop operations succeeded and were independently verified.
Praxis plan/packet integration, SWM-06 and full-runtime qualification remain open.

The GHL-03 entrypoint, explicit permission/model selection, uncertainty handling,
frozen outputs and usage observations are now covered by the fresh confirmation
above. Publication remains subject to GHL-04 and the pending destination selection.

## GHL-01 developer-preview candidate — 29 October 2026

- Replaced the inherited landing page with an attributed preview README and
  [quick start](QUICKSTART.md). Added a single offline demo command that preserves
  raw test logs, returns failures and refuses to overwrite earlier evidence.
- All four selected tests passed once, without skips (8.038s). Real helper-process
  overlap is distinguished from fake-runtime tests; no AI workers were launched.
  Parser checks and controlled runner-failure checks also passed, including
  nonzero exit propagation and existing-output preservation.
- Added a [portable result](evidence/offline-demo.json), third-party declarations
  and a [publication boundary](PUBLICATION.md). Original license declarations
  remain intact. Scope reconciliation, inherited-file modification notices and
  full publication/history scanning remain GHL-04 work; this is not clearance.
- Restricted inherited deployment, bot comments and vendor polling to upstream.
  Independent review found no blockers in the runner, claims or workflow guards.
- Confirmed GitHub account `sparkplug604` read-only. Proposed public destination
  `sparkplug604/praxis-swarm` awaits the user's selection; only `upstream` is
  configured. No remote was created or pushed.

Local archive: `outputs/swarm-build/ghl-01` (not distributed), with the original
125-file preservation manifest, original README/progress, raw demo and failure
checks, final source hashes and preservation results. Existing game work and
prior evidence are preserved. GHL-04 must complete its release checks before
publishing even if the developer preview ships ahead of live integration.

## Full-runtime qualification gate (retained)

**SWM-01: preparation layer implemented/tested; live qualification remains open.**
**SWM-02: implemented and validated; all selected test cases completed.**
**SWM-03: local planning and packet composition implemented and tested.**
**SWM-04: local routing and usage admission implemented and tested.**
**SWM-05: local controller, workspace copies, exact launch/receipt bridge, fenced
cancellation and Mac offline diagnostics implemented; containment and live wiring remain open.**
SWM-00's partial baseline remains recorded below. B-01's Mac Git-include defect
is repaired and the complete adapter package passes. No product model sessions,
production integration or publishing were started in this work package.

The deferred next strict-runtime implementation is **an offline descendant-containment diagnostic for
Linux-compatible tasks using a pinned container inside Docker Desktop's guest**.
The installed CLI is a candidate, not a qualified engine or runtime. Begin with
read-only engine/image inventory and explicit guest/container identity; do not
silently route Mac-only tasks to Linux. See the [boundary decision and next
requirements](MAC_RUNTIME_DIAGNOSTICS.md#next-containment-diagnostic).
Generation-fenced host stop and receipt-backed scheduler cancellation now work
in test mode. A native detached child still survives an acknowledged PTY stop;
pinned probes therefore withhold terminal absence and keep capacity reserved.
Do not enable live dispatch through this exact-copy path or substitute ordinary
`Manager.Spawn`/`Manager.Kill` as if it met that contract. The separately scoped
supervised AO profile above may use inherited session/worktree behavior with its
limits recorded. Do not restart the completed receipt-hash work.
SWM-05 live acceptance and SWM-06's parallel model demonstration remain open.
Development subagents, fake runtime tests and controlled helper processes are
not live product swarm evidence.

## SWM-01/05 generation-fenced cancellation — 29 October 2026

- Preserved all 108 previous source hashes before edits, existing evidence and
  game changes. Separate agents implemented the host protocol and cancellation
  controller, with an independent boundary review.
- Pinned hosts now check immutable session/generation identity before closing
  their PTY. They reject legacy kill requests. Concurrent and repeated requests
  share one retained close result; cleanup failures reach the caller. The host
  listener and registry stay available for explicit receipt recovery.
- The pinned stop client accepts recovered ownership through the exact host
  acknowledgement. Missing, changed, malformed or unreachable identity retains
  uncertainty. It does not fall back to PID signaling or ordinary destruction.
- Scheduler cancellation requires an already cancelled durable run, current
  ownership/lease and matching immutable execution receipts. Lease takeover
  cannot change the launch generation. Every acknowledged stop is observed;
  PTY cleanup alone never releases capacity. Pending/ambiguous/multiple-handle
  outcomes remain held. A final single-use `no_effect` result can finalize
  without a runtime stop.
- Review caught diagnostic teardown still sending a now-refused legacy kill.
  The correction uses only its original owned process to request graceful host
  disposal; it is separate from product stop/recovery. Stale/recovered diagnostic
  destruction remains rejected.
- Native worker/reviewer restrictions still pass. A detached child survives the
  new exact stop while protected writes remain denied; receipt replay succeeds,
  the probe remains unknown, and fixture-specific cleanup confirms its removal.

Evidence: fenced-stop directory (local archive: `outputs/swarm-build/swm-05-fenced-stop`; not distributed),
including native/race logs, independent review, final validation, source hashes
and preservation checks. No new schema, provider request, model session,
container/VM launch, daemon/UI dispatch, publishing or production integration.
Live execution remains disabled. Full-run cancellation orchestration, reserved
attempt cancellation and durable stop receipts surviving host death are not
implemented. The previous baseline test limitations remain recorded below.

| Check | Result |
| --- | --- |
| Final full ConPTY / registry race suites | Passed, 20.321s / 1.344s |
| Final Mac sandbox race suite | Passed, 4.970s; detached containment still fails as expected |
| Full scheduler race suite / final affected cancellation suite | Passed, 58.496s / 24.394s; final authority checks also passed |
| Diagnostic disposal regressions, race / non-race | Passed, 3.037s / 1.759s; timeout evidence retained |
| Full backend build / affected-package vet | Passed on settled source |
| Prior source preservation / game status | 101 of 108 prior files unchanged; seven intentionally updated; game status unchanged |
| Live provider and container qualification | Not run; live dispatch disabled |

## SWM-01/05 Mac runtime diagnostics — 29 October 2026

- Preserved all 99 prior source hashes, progress and game status. Parallel work
  implemented the pinned host and reviewed native cancellation; the coordinator
  implemented sandbox configuration and real offline worker/reviewer fixtures.
- Added one explicit direct-PTY runtime using AO's shared Unix spawner, with
  exact environment/argv/cwd, pinned host bytes and disposable registry scope.
  Loader overrides, global `.ao` aliases, registry/host drift, recovered cleanup
  and session-ID reuse reject. Possible startup effects remain recorded.
- Added a narrow Mac sandbox policy bound to the actual runtime, separate
  workspace/scratch/protected/registry roots and explicit tools. Physical aliases,
  root replacement and pre-existing links/special files reject. Worker writes
  and reviewer read-only behavior are tested through native host processes.
- Review tightened denial assertions to permission errors, independently checked
  aliases, and child readiness/result markers. It also prevented a native close
  change from signaling an old numeric process group after its leader was reaped.
  Parent-first exit remains uncertain; it is not silently treated as cleanup.
- A real detached descendant survived host cleanup while retaining filesystem
  restrictions. The probe returned unknown; fixture-specific cleanup then proved
  that test process absent. This is **failed process containment qualification**,
  correctly detected by a passing regression test.

Evidence: Mac runtime directory (local archive: `outputs/swarm-build/swm-05-mac-runtime`; not distributed).
Final validation and preservation receipts are recorded there. No live models,
provider calls, production integration, daemon/UI wiring or publishing occurred.
All changes remain uncommitted. The existing broad baseline limitations remain.

| Check | Result |
| --- | --- |
| Full ConPTY/registry race suite after final cleanup correction | Passed, 13.317s / 1.261s |
| Final Mac sandbox package, race detector | Passed, 4.946s; 23 checks per worker/reviewer role |
| Stubborn descendants with a live original parent | Cleanup verified in native fixture |
| Parent-first exit | Uncertain group refused without signaling; fixture cleanup verified separately |
| Detached descendant | Survived host cleanup; retained file restrictions; unknown probe; containment failed |
| Full backend build / affected-package vet | Passed after settled source changes |
| Prior source preservation / game status | 97 prior files unchanged; only two prior documentation files updated; game status unchanged |
| Live provider, daemon/UI and production qualification | Not run; remain disabled |

The outer sandbox initially blocked nested `sandbox-exec` and a native loopback
bind. Authorized native execution allowed the disposable diagnostic to apply its
own worker policy. Two restrictive shell probes then exited 134 until the root
directory read required by the dynamic loader was added. Initial failures and
superseded diagnostic runs remain visible; they are not completed qualification.
An intermediate native suite caught a signal transition error after the original
group had already disappeared. Teardown now accepts independently confirmed
absence and retains signal errors when absence remains uncertain; the final
suite passed and the failed run is preserved.

At that checkpoint, `Runtime.Destroy(handle)` had no atomic generation fence and
host shutdown discarded native close errors. The fenced-stop work above addresses
those stop-protocol gaps; ordinary Destroy is still not the swarm stop path.
Process-group cleanup still cannot contain `setsid` descendants.
The sandbox is offline and uses Apple's private interface; it is not provider
qualification or protection against an arbitrary concurrent same-user host actor.
Do not turn these diagnostics into a live permission receipt.

## SWM-01/05 exact bridge — 29 October 2026

- Preserved all 86 prior SWM-05 source hashes, progress and game status before
  editing. Separate agents implemented receipt persistence, reviewed the AO
  runtime boundary and tested the launcher; the coordinator connected planning,
  qualification and dispatch. Existing uncommitted work remains preserved.
- Split stable capability profiles from full prompt/session qualification hashes.
  Added dispatch-only packet revalidation, frozen catalog-scope checks, exact
  model/effort/workspace binding and explicit command-based prompt delivery.
- Added an internal session-manager bridge to AO's existing Runtime port, with
  no interactive workspace provisioning, settings inheritance, naming calls,
  cleanup or automatic retries. It has no ordinary AO session/UI row yet.
- Added immutable final execution receipts and results in the existing SQLite
  database. One atomic authorization permits one Create call. Late outcomes
  survive cancellation/takeover; possible handles and joined-error ambiguity
  are retained. Unknown observations keep capacity reserved.
- Independent review corrected clock-sensitive replay, catalog-scope drift and
  joined errors that could hide a possible fallback process. Regression tests
  cover time-advanced replay, changed inputs, concurrent authorization, late
  persistence, source preservation and final configuration cloning.

Validation: bridge evidence (local archive: `outputs/swarm-build/swm-05-bridge`; not distributed), including
`validation-summary.json`, `implementation-review.json`, `preservation-check.json`
and the final `source-sha256.json`.

| Check | Result |
| --- | --- |
| Qualification and planning packages, race detector | Passed; profile/final binding and dispatch revalidation covered |
| Scheduler package, race detector | Passed, 55.494s; final exact-bridge/scope/replay checks passed separately, 26.357s |
| Session-manager exact-launch and joined-effect checks, race detector | Passed, final combined targeted run 1.666s |
| Execution receipt storage, race detector | Passed, final 22.506s; competing stores and additional-handle restart included |
| Receipt domain and migration/ledger checks, race detector | Passed; final domain 1.374s, migration/ledger 20.046s |
| Full backend build and affected-package vet | Passed after final scope correction |
| SQL reproducibility | Cached exact sqlc v1.31.1 reproduced 39 generated files unchanged |
| Preservation and formatting | Passed; unrelated prior source hashes and game status unchanged |
| Live models, OS sandbox, descendant cancellation, daemon/UI integration | Not run/qualified; live mode remains disabled |

Initial diagnostic runs exposed two test expectation mistakes (unsafe default
permission mode and the expected unknown-probe error) and a compile-time routing
field mistake. These were corrected; failed logs remain alongside final results.
The observed joined-runtime-error defect was fixed conservatively instead of
claiming an uncertain process was absent. A process listing used to inspect test
completion required sandbox escalation; it made no runtime/configuration changes.

The bridge hashes the supplied environment overlay, not the full inherited child
environment. Its request/inspector callbacks remain trusted coordinator inputs.
The current recheck requires the same immutable permission receipt and expiry;
a concrete verifier must define stable receipt lookup/renewal before live wiring.
All launch checks here used fake providers/runtime ports; the earlier controlled
helper-process overlap remains separate evidence. The existing broad baseline
suite limitations remain unchanged. No game/Q1/Q2 or global Praxis files changed.

## SWM-05 local work — 28 October 2026

- Preserved the SWM-04 source manifest, progress and game status. Parallel
  assignments implemented filesystem snapshots, durable capacity and AO boundary
  analysis; the coordinator implemented scheduling and integrated their results.
  Subagents reached their usage limit during final review. The coordinator
  completed remaining fixes and validation; no substitute models were launched.
- Added one immutable scheduler pool, separate worker/reviewer slots, canonical
  account/provider and project limits across runs, and atomic original/resolved
  resource reservations. Unknown process ownership and expired leases retain
  capacity. Database restart and competing stores are covered.
- Added independent working-file copies preserving tracked dirty input, untracked
  and explicitly selected ignored inputs, modes and raw index bytes. Physical
  path identity handles actual case/Unicode aliases and nested repositories.
  Source drift, hardlinks, symlinks, special files, root replacement and copies
  inside linked-worktree/common Git metadata reject.
- Added rotating project queues, concurrent test dispatch, operation replay,
  owner checks, preparation revalidation, launch/reconciliation exclusion and
  terminal-absence requirements. Review corrected replay starvation, stale-base
  launch, foreign-owner replay, FIFO open races, ignored-input semantics and the
  Git-metadata destination hole. See [contracts and remaining bridge work](SCHEDULING_AND_WORKSPACES.md).

Validation: SWM-05 evidence (local archive: `outputs/swarm-build/swm-05`; not distributed), including
`validation-summary.json`, `implementation-review.json`, `preservation-check.json`
and `source-sha256.json`.

| Check | Result |
| --- | --- |
| Workspace and scheduler packages, race detector | Passed, 13.650s and 43.678s after metadata/owner fixes |
| Final owner/runtime classification regression, race detector | Passed separately after final guard |
| Scheduler storage contention/capacity regressions, race detector | Passed, 20.686s; broader swarm storage/domain regressions also passed during implementation |
| Scheduler migration preservation/immutability/rollback, race detector | Passed, 18.008s; earlier selected history checks also passed |
| Routing replay and changed schedule identity, race detector | Passed, 18.070s full routing; 18.123s targeted schedule/replay checks |
| Actual filesystem alias behavior | Passed on normal Mac volume and disposable case-sensitive APFS volume; dedicated volume log has no skips |
| Overlapping execution | Two real controlled helper processes in independent copies; test-only composer/qualification inputs, no model sessions |
| Full backend build and affected-package vet | Passed |
| SQL regeneration, formatting, preservation | Passed; unrelated prior source hashes and game status unchanged |
| Live AO bridge, enforced sandbox, UI workflow, live multi-model swarm | Not implemented/qualified; no success claim |

The first sandboxed disk-image creation failed. An authorized escalated creation
and mount enabled the case-sensitive test; the volume was detached afterward.
DiskManagement metadata inspection was unavailable in the sandbox; behavioral
test and hdiutil operation evidence are retained instead. Normal-volume final
tests may skip the separate case-sensitive fixture; its dedicated successful run
remains separate evidence.

The functional scheduler path currently requires an explicit test runtime. Live
runtime mode rejects before reservation. Real planning packet composition remains
independently covered by SWM-03; the new controller tests use a composer double.
Index recovery still depends on the original Git object store. Snapshots must be
retained by the coordinator; filesystem checks alone provide no confinement.
Existing baseline suite limitations remain unchanged. All code is uncommitted;
game/Q1/Q2 and global Praxis artifacts were not edited.

## SWM-04 work — 28 October 2026

- Added pure route advice using AO's existing model catalog, explicit role/model/
  effort policy, context-capacity checks and separately scoped quota windows.
  Unknown usage remains visible; expired observations require refresh. Neither
  explicit overrides nor spare quota bypass model bounds or upgrade models.
- Bound routing advice to immutable attempts and existing SQLite task claims.
  Account-wide, meter-specific, total and premium commitments are checked inside
  the claim transaction. One account cannot evade its commitments through another
  bucket. Request replay recovers the original operation without new discovery or
  reservations; it grants no launch permission.
- Added immutable response receipts and deduplication, nullable usage facts,
  failed/cancelled/review accounting, late-usage holds and model-mismatch blocking.
  Added an explicit cosmetic-call opt-out through delegation and session-manager
  naming paths while preserving non-swarm defaults.
- Parallel development and cross-review corrected late-receipt loss, runtime
  model mismatch omission, replay after quota refresh, incomplete quota scope
  reporting, non-finite percentage hashing, and unknown-usage bypass of known
  capacity. See [routing contracts and limits](ROUTING_AND_USAGE.md) and
  [source provenance](UPSTREAM.md).

Validation receipts: SWM-04 evidence (local archive: `outputs/swarm-build/swm-04`; not distributed), including
`validation-summary.json`, `implementation-review.json`, `source-sha256.json`,
`preservation-check.json`, SQL reproducibility and raw logs.

| Check | Result |
| --- | --- |
| Full domain, planning, qualification and routing packages, race detector | Passed; final quota correction separately rechecked with dependent routing/storage tests |
| All swarm storage tests, race detector | Passed, including independent-store contention, late receipts and settlement replay |
| Additive routing migration and migration ledger, race detector | Passed; existing project data and immutable account identity preserved |
| Affected delegation/title-generation tests, race detector | Passed in both session packages; prior defaults preserved |
| Full backend build and affected-package `go vet` | Passed |
| SQL generation | npm wrapper failed DNS lookup; cached exact sqlc v1.31.1 generated successfully and reproduced unchanged output |
| Preservation and formatting | Passed; unrelated prior source hashes and game status unchanged |
| Live provider usage collection, model dispatch, UI, measured savings | Not run; no claims made |

Usage snapshots and account identity are trusted coordinator inputs. Local
reservations are not provider-enforced caps. Provider observations plus local
spend may conservatively double-count until watermark reconciliation is added.
Policy/budget rollover and recovery from late overruns/model mismatch are not
implemented. The existing baseline timeouts and frontend/session-manager coverage
limits below remain open; unchanged full suites were not repeated. All changes
remain uncommitted. Game source, Q1/Q2 evidence and global Praxis were not edited.

## SWM-03 work — 28 October 2026

- Preserved the previous source manifest/progress and game status before edits.
  Two parallel development assignments covered plan validation and context
  selection; the coordinator implemented composition, tool checks and integration
  tests. Cross-review found and closed command/tool mismatches, incompatible
  version validation, FIFO blocking and state checks preceding slow verification.
- Added strict planner JSON, explicit interfaces/error/acceptance contracts,
  deterministic dependency/resource waves, and immutable work specifications
  inside the existing contract JSON. Legacy contract bytes/hashes are preserved;
  no new database, migration, SQL or global Praxis changes were needed.
- Added hash/approval-bound context selection, complete-envelope byte accounting,
  required-context failure and explicit whole optional omissions. OpenSwarm's
  matching/ranking concepts are attributed in [provenance](UPSTREAM.md).
- Added one-assignment packets from durable attempts, generated startup/stop
  instructions, current-state/tool/context checks on resume, and explicit new
  contract/attempt requirements for changed tool identities. Details and limits
  are in [planning and handoff contracts](PLANNING_AND_HANDOFF.md).

Validation receipts: SWM-03 evidence (local archive: `outputs/swarm-build/swm-03`; not distributed), including
`validation-summary.json`, `implementation-review.json`, `source-sha256.json`,
`preservation-check.json` and raw logs.

| Check | Result |
| --- | --- |
| Full planning package, race detector | Passed, 19.113s after command/tool binding fixes |
| Full domain package, race detector | Passed, 8.221s; includes historical contract hash compatibility |
| Existing swarm storage regressions, race detector | Passed, 20.092s |
| SQLite restart and explicit changed-tool migration | Passed; original packet rejects replaced binary, revised attempt pins its new hash |
| Full backend build; affected-package `go vet` | Passed |
| Independent code review | Four reported defects corrected; no remaining blocking findings reported |
| Preservation and formatting checks | Passed; game status and unrelated prior source hashes unchanged |
| Model dispatch, live worker comprehension, live Praxis eligibility, token savings | Not run; no claims made |

Fresh-worker coverage is automated inspection of a self-contained, single-task
packet plus rejection of contradictory/resealed startup instructions. Arbitrary
prose contradictions and prompt injection are not generally detectable by this
schema. The trusted context resolver is an interface with synthetic test fixtures,
not an installed approval service. Real source-manifest containment and dispatch
remain later gates. Existing baseline timeouts/incomplete suites below remain
visible; unchanged long suites were not repeated. All changes remain uncommitted.

## SWM-01/02 work — 28 October 2026

- Preserved all pre-existing setup changes in a checksummed copy before editing.
  Isolated workers handled qualification and the Git alias correction; the
  coordinator owned schema/storage. Independent reviewers found and helped close
  omitted launch-option binding, directory replacement during verification,
  expired submitted leases, and missing original Git-base provenance.
- Added explicit model/effort validation, per-role fallback policy, unknown versus
  reported identity, and runtime/configuration/permission evidence bindings.
  Test evidence cannot authorize live dispatch. No concrete live verifier or
  dispatcher is wired yet.
- Added the validated task DAG, immutable versioned contracts/attempt identity,
  atomic task/resource/usage claims, immutable dispatch operation IDs, lease
  generations, cancellation/reconciliation, review/evaluation and integration
  records. State uses AO's existing SQLite transaction and SQL generation paths.
- Added polling APIs and documented the trusted-caller boundary in
  [state and qualification contracts](STATE_AND_QUALIFICATION.md). Resource scopes
  are logical locks; actual filesystem containment is still required. Usage
  reservations are aggregate admission thresholds, not enforced provider caps.
- Regression checks cover concurrent independent stores, crash/restart without
  relaunch, rejected submission recovery, independent exact-commit evidence,
  contract revisions, rollback, overflow and additive migration data preservation.

Validation receipts: SWM-01/02 evidence (local archive: `outputs/swarm-build/swm-01-02`; not distributed),
especially `validation-summary.json`, `implementation-review.json`,
`source-sha256.json` and the preserved raw test logs.

| Check | Result |
| --- | --- |
| Full backend build; affected-package `go vet` | Passed |
| Full race tests: domain, qualification, Command Code, storage API | All four packages passed (8.387s, 1.484s, 3.370s, 209.223s) |
| Full SQLite race command | Timed out at 900.542s; 118 top-level tests completed; no assertion failures |
| Exact unfinished SQLite inventory | All remaining 14 passed under race detection in 120.590s |
| SQLite combined coverage | All 132 top-level tests completed across the preserved runs; not a single full-package pass |
| Additional cross-run/two-store resource and budget contention | Passed under race detection (18.249s) |
| Additive migration, immutability, rollback | Passed, including preserved pre-existing project data |
| SQL generation and diff checks | Passed |
| Public-boundary script | Fixed masked discovery failures; valid, failing and malformed-tool cases verified |
| Preserved setup checksums; game Git status | Unchanged |
| Live model/sandbox qualification; UI swarm dispatch; production integration | Not run |

Use at least a 30-minute SQLite package deadline for the next complete run. Do
not repeat the unchanged checks just to resume development. The timeout remains
visible; completing its missing inventory does not rewrite that failed command.
All changes remain uncommitted. Game files, Q1/Q2 evidence and global Praxis
state were not edited.

## Work and evidence — 28 October 2026

- Created an isolated `praxis-swarm` repository, branch `swarm/swm-00-baseline`,
  at AO `71bb19f5947a64853ee6d11225bc896563878c50`. Preserved all 3,628 reachable
  commits in a non-shallow, blob-filtered clone. Remote: `upstream`.
- All 5,416 snapshot files match the pinned checkout. Original downloaded sources,
  game changes and prior Q1/Q2 artifacts were not edited. Game Git status was
  compared before and after.
- Added local-tool bootstrap, scratch launch environment, portable build plan,
  provenance, and [Mac baseline instructions](MAC_BASELINE.md). No upstream Go or
  frontend application source was changed. Existing notices remain intact.
- Independent setup review caught an inherited daemon-binary override and Go
  build-flag ordering. Both were corrected in the local scripts. An optional
  Vitest progress reporter now preserves file identity for future diagnostics.
- Tools: Go 1.27.1, Node 24.22.2, npm 10.9.9, Electron 44.7.0, Darwin arm64.
  Dependency installs used lockfiles; application install hooks were initially
  suppressed and required native preparation was performed explicitly.

| Check | Result |
| --- | --- |
| Snapshot comparison / upstream history | Passed |
| `scripts/check-public-boundary.sh` | Passed; private cloud module absent |
| `go build -p 4 ./...` | Passed |
| `npm run typecheck` | Passed |
| Explicit local daemon build | Passed |
| Native SQLite Node and Electron builds | Passed separately; different ABIs |
| Actual Electron UI | Passed: Orchestrator development window, Get started workflow |
| Scratch daemon `/healthz` and `/api/v1/sessions` | Passed: correct executable/data path, zero sessions |
| Daemon `/shutdown` | Passed; endpoint subsequently refused connection |
| Normal GUI menu quit | Not verified; remaining UI required process cleanup |
| Frontend unit suite | Inconclusive: partial run passed 61 files / 4,539 tests, with 1 environment failure and 9 skips; clean full run stalled and was stopped |
| Cloud-auth file without lab environment | Passed: 35 tests |
| Diagnostic progress reporter | Passed on the 35-test cloud-auth file; emitted 38 events |
| Full Go race command with five-minute package limit | Finished, exit 1: 208 packages passed, 1 adapter assertion failure, 3 package timeouts |
| Go timeout packages | `service/chat`, `session_manager`, `storage/sqlite`; incomplete in this run |
| Earlier chat-package result | Passed in 450.500 seconds; its overall run was interrupted later |
| Go race detector | No data-race report observed; timed-out packages remain incompletely checked |
| Script syntax / flag forwarding / dependency locks | Passed; locks unchanged |
| Packaging, signing, Windows/Linux, agent adapters, live swarm | Not run |

Full local logs and receipts: SWM-00 evidence (local archive: `outputs/swarm-build/swm-00`; not distributed).
Keep interrupted and failed setup attempts; they are not completed baseline tests.
See [baseline findings](BASELINE_ISSUES.md) for the Mac adapter defect, environment
requirements, and the next diagnostic for the frontend stall.

Key receipts: `baseline-summary.json`, `go-race-tests-3.log` and its exit receipt,
`frontend-tests-4.interrupted.json`, `desktop-verification.json`, and
`tooling-review.json` in that evidence directory. Earlier attempts retain their
own status; their partial passes do not turn the final run into a clean pass.

The original SWM-00 next-work instructions are preserved in the SWM-01/02
`preserved-setup` evidence. Follow only the current gate above. The inherited
frontend stall and session-manager coverage remain open baseline limitations;
these unchanged areas were not re-run merely to restart this package.

## Setup findings and limits

1. Electron must be installed before tests import its Node package. An early test
   import tried its default user cache and failed. The environment now sets the
   installer-supported `electron_config_cache`.
2. SQLite compiled for Electron cannot be used by Node/Vitest. Rebuild for the
   intended ABI before each mode; preserve the setup failure as distinct evidence.
3. Long scratch paths break Unix sockets. Use a private short `/tmp` root for
   `AO_DATA_DIR`, `AO_RUN_FILE`, and `AO_DEV_ELECTRON_DIR` together.
4. An extra `sandbox-exec` profile conflicted with Chromium subprocess sandboxing.
   It was removed, not weakened by disabling Chromium's sandbox. The successful
   run is not an offline guarantee; upstream probes installed tools/accounts.
5. The app installer API recorded `brew install tmux`, including libevent,
   ncurses, utf8proc and jemalloc, and Homebrew cache cleanup. Its initiator was
   not established by this verification. This host change was not a bootstrap
   command. Do not describe the entire run as free of host changes or repeat the
   installer action. The install receipt remains in the evidence directory.
6. Electron's rebuild tool required its hardcoded user compiler caches; explicit
   filesystem access was granted for those two cache directories. Application
   data stayed in scratch storage.
7. Root Apache-2.0 and package MIT metadata conflict remains unresolved for
   redistribution. Local baseline work does not settle that question.

No AO worker sessions were dispatched, no model substitution was performed, no upstream push
or public fork was created, and no production integration was attempted.
