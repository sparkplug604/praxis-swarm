# SWM-00 baseline findings

These findings apply to pinned AO `71bb19f5947a64853ee6d11225bc896563878c50`.
They do not establish that our planned swarm is implemented or qualified.
[PROGRESS.md](PROGRESS.md) owns completion and eligibility.

## B-01: Command Code loses Git conditional includes through a Mac path alias

`TestPrepareSessionHomeAnchorsConditionalIncludeHeaders` fails at
`backend/internal/adapters/agent/commandcode/sessionhome_test.go:987` with the
normal macOS `/var/folders/...` temporary root. `user.email` is empty when the
copied `gitdir:./` condition should match the work repository.

The unchanged test passes with `TMPDIR=/private/tmp` and `-race -count=1`.
`/var` is a symlink to `/private/var`. The current `anchorConfigPattern` rewrite
constructs an absolute pattern without resolving the original config path.
This was strong evidence of an adapter portability defect at the SWM-00 baseline.

Carry into SWM-01 before qualifying Command Code: resolve the source config/home
base according to Git's include semantics, preserve the glob suffix, and add
symlink coverage. Changing only the test temporary directory would hide the issue.
No production source was changed during SWM-00 for this finding.

**SWM-01 correction:** the source config/home base now resolves according to
Git's physical-path rules for `gitdir` conditions; ordinary relative include
targets retain the logical config-file parent. Glob suffixes remain intact.
Real-Git oracle tests cover directory/config-file symlinks, negative matches and
ordinary include semantics. The complete Command Code package passed under race
detection on the normal Mac temporary path (3.370 seconds). Independent review
found no remaining material issue. Receipt: `swm-01-02/affected-race-tests.jsonl`
under the sibling `outputs/swarm-build` directory. This closes B-01's local
regression; it does not qualify Command Code's live worker permissions.

## B-02: Process tests need actual macOS process inspection

Restricted execution denied `/bin/ps`, causing failures in tmux integration,
preview-process ownership and zombie detection. The three complete packages pass
under `go test -race -p 1` when process inspection is allowed:

- `internal/adapters/runtime/tmux`
- `internal/previewserver`
- `internal/processalive`

These are environment-dependent results, not reasons to disable process-identity
checks or weaken cleanup ownership rules in the future swarm.

## B-03: Development state and test environment must be separate

All three app-state overrides are required; long paths also break Unix sockets.
SQLite must match the Node or Electron ABI. An inherited lab
`AO_CLOUD_AUTH_REDIRECT` changed one authentication test's expected URL. Its full
35-test file passes after removing runtime settings. The new check runner strips
`AO_*` variables and the launch script alone sets the lab configuration.

These were baseline setup errors, not upstream application regressions. Interrupted
runs and early setup failures remain in the local evidence for attribution.

## B-04: An empty-board launch has effects beyond scratch data

Startup performs installed-tool/account discovery. The app's installer receipt
also records a tmux Homebrew installation, dependencies and Homebrew cleanup; the
actor that initiated that UI action was not established by this verification.
Compiler preparation writes to Electron's hardcoded user caches. The successful
launch used standard Chromium sandboxing; an additional nested macOS sandbox
failed and was removed. Do not describe this baseline as offline or as making no
host changes. Worker permission containment remains an SWM-01 acceptance gate.

## B-05: Full frontend suite has no completed clean-environment result

The initial Node-ABI run stopped on the inherited redirect setting: 61 files and
4,539 tests passed, one test failed, and nine tests were skipped. Those totals
cover only part of the 595-file suite. The affected cloud-auth file subsequently
passed all 35 tests with the lab variables removed.

The clean-environment full run then stalled without reporting its current file.
A read-only sample showed the worker idle in libuv's event loop (1.08 seconds CPU
after 7:15 elapsed), rather than executing a CPU loop. Open handles and process
arguments did not identify the file; cached results predated the run. The
20-second test timeout did not bound this wait. Only the owned Vitest coordinator
and worker were terminated, and the wrapper returned 143.

This is **inconclusive**, not a passing suite or a diagnosed upstream defect.
Before repeating the full run, use the local `scripts/swarm/vitest-progress.mjs`
reporter (queued/start/end, optional test-start) and an external wall-clock deadline.
The reporter was verified on one file; the full stalled suite remains unresolved.
Retain the interrupted log and
termination receipt. Changes to frontend test configuration or runtime hooks
must carry this investigation; a partial baseline must not become a release claim.

## B-06: The bounded Go run does not finish large packages

The final full command used `go test -race -p 1 ./... -timeout=5m` and the chat
package exceeded that imposed limit (`300.618s`). The earlier run recorded this
same unchanged package passing in `450.500s`; that earlier overall invocation
was interrupted later and must not be called a full-suite pass.

Retain both results. The shorter timeout is a verification limit, not evidence
of a new functional failure. A future complete run needs at least the ordinary
ten-minute Go package deadline for this package, plus enough overall run time.
Do not patch application behavior merely to satisfy the shorter diagnostic bound.

`internal/session_manager` also reached the five-minute package limit
(`300.601s`); its current test had run for one second at cutoff. There is no
completed package result in this baseline. Its status is **inconclusive**. Before
qualifying changes to session dispatch/recovery, run the relevant tests and a
complete package check with a sufficient deadline; do not infer a pass from
elapsed time or partial progress.

`internal/storage/sqlite` likewise reached the five-minute limit (`300.499s`).
Its full validation is inconclusive; SWM-02 must verify migrations and concurrent
reservation behavior directly and complete the affected storage-package checks
with a sufficient deadline before claiming that work qualified.

**SWM-02 verification update:** four affected packages passed in full under race
detection, including `storage/sqlite/store` (209.223s). The complete SQLite run
reached even the extended 15-minute deadline at 900.542s, after 118 top-level
tests completed without assertion failures. Its exact remaining 14 tests passed
in 120.590s. The recorded inventory proves all 132 top-level SQLite tests were
completed across these runs; it does not convert the first command into a pass.
Future single-package verification should allow at least 30 minutes on this Mac.
Session-manager/chat coverage remains as recorded above; neither was changed or
requalified by SWM-01/02. See `swm-01-02/validation-summary.json` and raw receipts.

## B-07: Public-boundary check masked failed workspace discovery (repaired)

During SWM-01/02 verification, invoking the inherited script without the pinned
Go environment printed errors but still returned success. Bash process
substitution hid discovery's exit status. The script now captures workspace
discovery directly under `set -euo pipefail` before iterating. Checks verify that
valid tools pass, a failing Go command fails, and malformed workspace JSON fails;
neither failure prints success. Evidence: `swm-01-02/public-boundary-regression.json`.
The initial invalid check is retained as `public-boundary-no-toolchain.log` and
does not count as a pass. No dependency or license boundary was changed.
