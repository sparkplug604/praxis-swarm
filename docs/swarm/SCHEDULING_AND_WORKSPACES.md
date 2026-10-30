# Scheduling and workspace ownership

SWM-05 now provides a local scheduling controller, atomic concurrency limits and
filesystem-aware source copies and an internal exact-launch receipt bridge. Tests
use controlled local helper processes and fake runtime/provider ports.
**Live AO/model dispatch remains unavailable.** The existing AO spawn interface
cannot enforce the exact prepared workspace and final qualification receipt.
Follow [progress](PROGRESS.md); these tests do not complete the live SWM-05 gate
or SWM-06's multi-agent demonstration.

The [Mac runtime diagnostics](MAC_RUNTIME_DIAGNOSTICS.md) now exercise a pinned
direct PTY host and offline filesystem restrictions. Detached-child termination
remains unqualified; these helpers do not enable live scheduler dispatch.

## Local coordinator workflow

1. Configure SWM-04 routing/account budgets and observations, then call
   `ConfigureSwarmScheduler` before any active attempts. One immutable pool per
   database defines maximum worker and reviewer slots plus explicit provider and
   project limits. Other roles consume worker slots. Provider keys are canonical
   routing account IDs; changing model or harness cannot create extra slots.
2. Capture each candidate through `workspace.Capture`. Supply the Git repository
   root, a new destination outside it, and any explicitly required ignored paths.
   Retain the returned snapshot in coordinator-owned storage outside worker
   write access. Its source manifest, index bytes and destination identity are
   bound to the attempt; the snapshot is not a second task queue.
3. Supply `scheduler.Service.Tick` with candidates from all ready projects and
   stable logical request IDs. It rotates project queues, resolves filesystem
   resources, calls routing admission and dispatches admitted work concurrently.
   The store checks dependencies, task ownership, original semantic/path locks,
   resolved physical locks, account/project/role capacity and usage in one writer
   transaction. A blocked candidate does not end the pass.
4. A reserved attempt is reloaded, its packet freshly composed/validated, its
   workspace rechecked, and its launch prepared. `BeginSwarmDispatch` commits the
   unique operation before any external start. Only the winner starts it. Runtime
   errors after that boundary are ambiguous and retain reservations.
5. Reconcile by the exact operation and runtime identity. Unknown observations,
   failed probes, cancellation requests and lease expiry do not release slots.
   Confirmed terminal absence must rule out pending/future launch as well as a
   currently running process. Local exclusion prevents reconciliation while the
   same controller is starting the operation; a future distributed runtime must
   enforce the terminal fence across controllers too.
6. Before frozen submission, call `workspace.ValidateSubmission`. It accepts
   candidate content changes while checking roots, links and source preservation.
   This produces a content manifest, not an accepted Git commit or independent
   review. Freezing, review/evaluation and integration remain later stages.

The APIs are in `backend/internal/service/swarm/scheduler`,
`backend/internal/service/swarm/workspace`, and the existing SQLite store.
No new global database, board cutover, HTTP endpoint or frontend workflow was
introduced. Historical contracts retain their original bytes through omitted
optional fields. After scheduler configuration, new unscheduled claims reject;
historical request replay remains recoverable.

## Preservation and filesystem behavior

Capture includes dirty tracked files, untracked files selected by Git's normal
ignore rules, deleted tracked paths, file modes and explicitly selected ignored
inputs. It preserves raw index bytes and parsed stage entries. Git history and
staged object blobs remain in the original object store; this is not a standalone
Git backup. Required ignored build/tool inputs must be selected explicitly.

Each working copy has independent files, with no shared Git metadata or hard
links. Existing destinations and source/destination overlap reject. Source/index
drift during capture, launch or collection rejects instead of silently changing
the baseline. Failed captures retain their exclusive partial directory for
inspection; cleanup is a separate coordinator action.

Resolution uses actual directory entries and file identities, including the
volume's case and Unicode behavior. Canonical absolute path scopes also conflict
across nested repository roots. Missing paths conservatively lock their nearest
existing directory, so parallel work may serialize more than expected. Raw
resource keys remain trusted coordinator inputs; the store does not discover
filesystem aliases itself.

Symlinks, hardlinked regular files, special files, split indexes and submodules
are currently rejected. These conservative restrictions need explicit support
before such repositories are eligible. Windows filesystem identity is unsupported
in this implementation. Boundary scans do not enforce a sandbox or stop a worker
from accessing other paths; live launch requires separate effective confinement.

## Replay, fairness and capacity

Replay recovers the existing operation before inspecting a worker tree that may
already have changed. It must still match the frozen policy, source/workspace
identity, task version and request settings. Another owner requires explicit
lease takeover. Nonlaunchable replays do not consume the pass admission limit.
An attempt's durable base must match its source snapshot even if integration
advanced between initial validation and admission.

Worker/reviewer limits span runs; provider and project limits are independent
allowlists. Unknown ownership occupies capacity even after its lease expires.
Confirmed stopped attempts release execution capacity and resource locks, while
unknown token usage remains reserved under SWM-04. Policy changes and pool
rollover are not implemented.

Fairness is round-robin within the candidates supplied to one coordinator. Its
cursor resets on restart; SQLite still preserves identity and capacity. This is
not a durable distributed fairness guarantee. Admission/preparation deadlines are
separate from worker lifetime, and collaborators must honor context cancellation.

## Exact launch bridge and receipts

The internal `scheduler.ExactPreparer` / `ExactRuntime` path now connects real
planning and qualification services to `session_manager.SwarmExactLauncher` and
AO's existing `ports.Runtime.Create`. It currently accepts test evidence only.
It creates no ordinary interactive AO session row and is not daemon/UI wiring.
`SpawnConfigFor` remains a diagnostic mapping; ordinary `Manager.Spawn` still
cannot enforce this copied-workspace contract. The live scheduler gate remains
closed before reservation.

Qualification has two identities. `CapabilityFingerprint` pins the role, catalog
scope, selected settings, runtime identity, permissions, tools, directory identities
and launch profile, excluding task prompt and session/native session IDs. It can
therefore be known before the packet embeds it. `Binding` additionally includes
the complete prompt, session IDs, adapter command and prompt-delivery strategy.
Neither fingerprint alone grants permission. Changing the frozen routing catalog
scope is rejected before final preparation; storage binds the reserved account,
and actual credential-to-account
mapping is still a trusted inspector/request-provider responsibility.

After `BeginSwarmDispatch`, dispatch-specific packet validation rechecks context,
tools and current contract without letting a worker resume a pending launch. The
bridge preserves the exact prepared command/workspace and adds only the explicit
operation-derived `AO_RUNTIME_LAUNCH_ID` to the environment overlay. Its final
receipt binds the packet, qualification, source and workspace, session, generation,
permission receipt, expiry and complete `RuntimeConfig` hash. Raw argv/environment
values are not persisted in the receipt table. The immutable receipt must be
atomically authorized before `Create`; only the first caller receives permission
to call it. Replays return the original outcome, including after evidence expiry
or worker-file changes, and never reacquire launch permission.

`RecordSwarmExecutionResult` preserves late outcomes even after cancellation or
lease takeover. Ambiguous outcomes, missing outcomes, and uncertain probes retain
capacity. Joined runtime errors are conservative: one branch claiming no effect
cannot erase another branch's possible process. Primary and additional runtime
handles are retained. A failed request does not invoke runtime destruction or
workspace cleanup. Caller cancellation does not cancel the bounded outcome-save
attempt; a save failure is returned explicitly and leaves durable launch intent.

The first supported diagnostic prompt is delivered in the command. Post-start
prompt delivery, provider-native resume IDs, delayed-on-attach startup and recovery
shells are excluded. Fresh launch revalidation still cannot close an OS-level
filesystem race by itself.

## Remaining live integration

Test-mode cancellation is implemented through `Service.Cancel` →
`ExactRuntime.Stop` → `ports.FencedRuntimeStopper`, after durable run cancellation
and current lease checks. It uses immutable execution receipts, never ordinary
`Destroy`/`Manager.Kill`. Pending or ambiguous launches retain capacity. Every
acknowledged stop is observed; the pinned Mac runtime still returns unknown for
terminal absence because detached children can survive PTY cleanup. See
[the stop contract and containment gate](MAC_RUNTIME_DIAGNOSTICS.md).

- Freeze one concrete backend. AO's hybrid runtime can retry through tmux after an
  ambiguous direct-PTY error; it must not be used for qualified swarm execution.
- Connect the diagnostic pinned host/registry/full-environment binding to final
  launch preparation. The general bridge currently hashes an environment
  **overlay**, not the full child environment. Direct PTY adds terminal variables
  and uses the current AO executable as its host.
- Qualify filesystem and descendant-process confinement, protected coordinator/
  evaluator denial, reviewer write denial, and cancellation on a disposable Mac
  fixture. Test receipts and native approval flags are insufficient.
- Qualify cancellation and recovery with a real descendant-containment boundary.
  The test-mode exact stop path checks host generations, while unsupervised tmux
  cannot supply equivalent proof. Ambiguous starts require ownership recovery,
  not an automatic retry. Full-run cancellation and reserved-attempt handling
  still need lifecycle orchestration.
- Add daemon/session lifecycle adoption without ordinary AO Kill/Restore/Cleanup
  deleting copied evidence or starting a replacement process. Until then these
  internal bridge executions have no ordinary session/UI row.

These are the next SWM-01/05 gates before SWM-06's overlapping model demonstration.
Controlled helper-process overlap and fake-runtime bridge tests establish local
behavior only; no model sessions or live sandbox checks were run for this bridge.
