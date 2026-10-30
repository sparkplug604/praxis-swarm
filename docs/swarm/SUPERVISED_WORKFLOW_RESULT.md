# Connected supervised workflow: confirmation and retained diagnostic

## Fresh confirmation

The user authorized a fresh trial of the corrected prompt on 29 October 2026.
The actual CLI completed two Luna/low assignments in separate worktrees pinned to
commit `22799e4e3922f0a6b1754bc13b54c3b010d848f1`. Their AO provider-turn intervals
overlapped by 12.141718 seconds; native task events show 12.100 seconds. Both
native turns completed with on-request approvals, workspace-write and network
disabled. Neither needed the earlier local completion-report escalation.

The combined frozen code passed 13 test methods on Python 3.9.6 and 3.14.3.
Source preservation, candidate hashes and disjoint file scope were independently
checked; read-only review found no concrete code defect. Both controllers were
observed stopped; the owned daemon exited 0 and its listener/run file disappeared.
The initial evaluator's Python 3.9 timestamp parse failed before tests; running
that unchanged harness under Python 3.14 resolved it. Candidates and oracle stayed
frozen, and both target Python runtimes were tested.

AO system instructions measured **1,289 bytes per worker**, versus 15,821 in the
partial trial. Native developer messages measured 32,615 characters, versus
47,130. Worker totals were 263,032 input tokens (216,960 cached) and 3,075 output,
266,107 total. Coordinator/review use and billing are excluded. These two runs
are not a controlled efficiency comparison and do not establish general savings.

The [sanitized confirmation](evidence/supervised-workflow-confirmation.json) is
separate from the retained earlier evidence. No new global configuration, billing
route, substitute model, production merge or automatic retry was used. This
completes GHL-03's narrow supervised CLI gate. GHL-04 release checks and destination
selection are next; full SWM-06 qualification remains open.

## Earlier partial trial

On 29 October 2026, the actual `ao swarm run` CLI launched two Luna/low workers
from a supplied plan through the new API/service path. They used separate AO
worktrees pinned to the supplied source commit. Their provider-turn intervals
overlapped by 19.408745 seconds. Native telemetry confirmed the requested model,
effort, on-request approvals and workspace-write. No alternate provider, new
billing route or global configuration change was used.

**The run ended `stopped`, not `completed`.** The formatter completed normally.
The parser implemented its helper and passed its named tests, then followed
inherited AO instructions to post `ao report --done`. That local HTTP call was
denied by the native network sandbox; the agent requested escalation and waited
for approval. The daemon's 240-second deadline interrupted it. The persisted
parser turn is failed. We did not approve on the user's behalf or retry the run.

The controller stop and preservation behavior worked. Both controllers were
independently observed stopped, their worktrees remained, source files were
unchanged, and frozen candidate hashes were independently checked. The combined
frozen implementation passed 13 test methods on both Python 3.9.6 and 3.14.3,
including 1,010 exact-value/roundtrip cases and large-input/type-error checks.
Those tests assess the toy duration contract, not general agent quality.
The owned daemon exited 0; its health endpoint and run file were absent afterward.

The [sanitized result](evidence/supervised-workflow-trial.json) distinguishes
successful candidate tests from incomplete provider completion. It retains
session/turn identities, native telemetry, hashes, permissions and usage without
personal paths or raw native instructions. Raw artifacts remain in the local
GHL-03 evidence archive and must not be copied into a publication indiscriminately.

## Correction after the partial trial

Fresh supervised starts now use a compact task-specific AO prompt: implement the
packet, run its checks, return a native final response and stop. They do not import
the ordinary AO reporting, delegation or pull-request workflow. Configured project
rules and sandbox/approval boundaries remain. Ordinary AO workers are unchanged.
This is an internal fresh-spawn profile; manually restoring its session through
ordinary AO restore is outside the supported workflow.

The correction initially had focused regression coverage only. Its fresh live
confirmation is recorded above; the earlier trial is not relabelled as testing
the correction. Both the recorded earlier binary and pre-correction source are
preserved. Full SWM-06 qualification and publication remain open.

## Earlier trial usage limits

The trial reported 359,454 worker input tokens (304,768 cached) and 3,340 output,
362,794 total. Cached input is included in input totals. Coordinator/reviewer use,
billing cost and savings are unknown. These are cumulative conversation counters,
not the size of a single prompt.

Actual task prompts were 7,689 and 7,604 bytes; each AO system prompt added 15,821
bytes. Native developer messages contained 47,130 characters, plus 17,730 base
instruction characters. A bounded task packet therefore does not bound all model
context. The confirmation above measures the corrected prompt and observed worker usage;
remaining native context is still substantial.

Run instructions: [supervised CLI guide](SUPERVISED_WORKFLOW.md). The only current
next-task authority is [PROGRESS](PROGRESS.md).
