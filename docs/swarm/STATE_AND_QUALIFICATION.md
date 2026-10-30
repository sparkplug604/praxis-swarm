# State and qualification contracts

Implementation reference for SWM-01/02. [PROGRESS.md](PROGRESS.md) owns status and
the next gate. These APIs do not start a swarm from the desktop or CLI yet.

## Ownership and boundaries

AO's existing SQLite store owns durable swarm state. There is no second database,
board or progress authority. Domain types live in `backend/internal/domain/swarm.go`;
store methods in `backend/internal/storage/sqlite/store/swarm_store.go`; launch
preparation in `backend/internal/service/swarm/qualification/`.

The deterministic coordinator is the trusted caller. Workers must never receive
write access to its database, verifier inputs or receipts. The current APIs do not
authenticate caller-supplied actor IDs, execute an oracle, enforce filesystem
permissions, stop a process or merge Git commits. SWM-05 through SWM-09 must bind
those operations to actual owned runtime sessions and protected evidence. A fake
receipt cannot be treated as live qualification.

## Model and permission preparation

`qualification.Service.Prepare` consumes AO's refreshed model catalog, existing
agent adapter, a runtime inspector and a trusted permission verifier. It preserves
explicit requested and resolved model/effort settings. Fallbacks are ordered,
explicit and per role; only definite missing adapter/model results permit them.
Authentication errors, unsupported effort, cancellation and uncertain capabilities
stop preparation. No model request, retry, login or provider substitution occurs
inside this service.

The stable capability profile excludes the task prompt and session IDs; the final
qualification binding includes them, avoiding a circular packet hash. Execution
receipts separately bind the exact RuntimeConfig consumed by the launch bridge;
see [the launch protocol](SCHEDULING_AND_WORKSPACES.md#exact-launch-bridge-and-receipts).

Evidence binds the role, catalog inputs, binary path/hash/version, effective
configuration, launch arguments/options/environment and directory identities.
Changes invalidate that binding, without invalidating another adapter's evidence.
Directories are checked again after verification. The future dispatcher must
still enforce and revalidate the actual sandbox boundaries at launch: a preparation
receipt does not close subsequent filesystem races.

Test-mode evidence returns `ErrUnverified` with `TestVerified=true`; it cannot
authorize live dispatch. Live admission requires unexpired matching evidence of
settings preservation, observed cancellation, process/descendant containment,
protected-storage denial and read-only submission access for a reviewer. This
package currently ships no concrete live inspector/verifier or dispatcher wiring.
Native approval flags alone do not prove confinement. AO's default Codex command
can emit a sandbox bypass; preparation rejects that command explicitly.

Reported model/effort remain unknown until supplied by actual runtime telemetry.
`RecordReportedIdentity` records a mismatch and removes live admission. Missing
telemetry does not invent an execution identity. Test doubles exercising the
live-receipt branch are still unit tests, not live-adapter evidence.

## Durable run and claim lifecycle

1. `CreateSwarmRun` normalizes protocol-v1 plans, validates the complete DAG and
   stores task-version snapshots. Initial versions are 1. IDs and normalized
   request hashes provide idempotency; a reused key with different intent fails.
2. `ClaimSwarmTask` checks current dependency acceptance, contract version,
   capacity, all resource conflicts and the shared usage bucket in one transaction.
   It stores an immutable attempt ID, operation ID, contract/hash, original Git
   base, requested/resolved identity, capability fingerprint and usage reservation.
3. `BeginSwarmDispatch` grants dispatch once, changing `reserved` to `dispatching`.
   The exact bridge then consumes `AuthorizeSwarmExecution` once for its final
   receipt before `Runtime.Create`; only its atomic winner calls the runtime.
   Commit the transaction before calling AO. Reconcile the immutable operation ID
   after a crash. Do not reissue a launch because the response was lost.
4. `ConfirmSwarmRunning` associates the provider-confirmed session. Nonempty
   session IDs are unique across attempts. Unknown reported model stays empty.
5. `FinishSwarmAttempt` requires a current owner/generation and coordinator-confirmed
   stop or absence. `submitted` additionally requires a running session and frozen
   commit. Failed/cancelled submissions retain their original commit and evidence.
6. Independent protected checks call `RecordSwarmEvidence` with exact attempt,
   contract, oracle and submission bindings. A worker cannot be its own reviewer
   by the recorded actor/session identity. Any failed/inconclusive evidence blocks
   acceptance for that attempt; close it and create a new attempt for corrections.
7. The future integration controller calls `AcceptSwarmIntegration` only after
   protected combined checks and actual Git integration succeed. Its record binds
   the base/result commits, attempts, independent combined actors and artifact
   hashes. Storage requires per-attempt independent passing review/evaluation,
   rejects stale heads/contracts and atomically advances dependency acceptance.

This is an internal storage API, not a worker-accessible HTTP endpoint. Recorded
combined-check hashes are evidence references supplied by the trusted controller;
storage cannot establish that their referenced artifacts or Git merge exist.

## Recovery, cancellation and accounting

Every mutation obtains SQLite's writer reservation before reading admission state.
The per-store mutex protects local calls; the database lock protects independent
store handles/processes. No transaction remains open for a model or runtime call.

Lease expiry never frees resources, erases usage, marks success or authorizes a
replacement execution. `TakeOverSwarmLease` increments generation and preserves
operation identity/state. Old generations cannot complete the attempt. Submitted
attempts can also transfer ownership to finish review failure or cancellation;
they cannot launch again. `RenewSwarmLease` does not shorten a lease.

`CancelSwarmRun` blocks new claims, dispatch and acceptance. It does not assert
that an in-flight process stopped. Reconcile it, then finish as cancelled.
Unknown launch outcome retains reservations until the coordinator can establish
stop/absence. Unknown usage remains reserved even after the process stops.

`SettleSwarmUsage` accepts one idempotent actual-token receipt per attempt after
execution ends. Shared bucket IDs must reflect the actual allowance/account, not
model names. Actual overrun is recorded honestly and blocks additional admission.
Overflow is rejected. Reservation limits are admission thresholds, not provider-
enforced hard spending caps. Per-category token/cost/account-window accounting and
review/integration admission belong to SWM-04; never label this aggregate as those
complete capabilities.

## Versioning and scope

`ReviseSwarmPlan` uses expected plan version and appends changed task versions.
Old contracts/attempt identities/evidence remain immutable. Changed predecessor
versions must propagate through dependent contracts before acceptance is possible.
The initial API conservatively rejects revision while any reserved, dispatching,
running or submitted attempt remains; unrelated concurrent revision is future
work. Task removal requires a later explicit archival operation.

Path resources conflict when equal or ancestor/descendant; semantic resources
conflict by exact key. The namespace is shared across runs. These are normalized
logical scopes, not a filesystem sandbox. SWM-05 must derive canonical repository
identities and handle symlinks/case aliases before constructing scopes. An agent
must not choose a fresh namespace to evade an existing reservation.

## Read APIs and next consumers

`GetSwarmRun`, `ListSwarmRuns`, `GetSwarmContract`, `GetSwarmAttempt`,
`ListSwarmAttempts`, `ListSwarmDispatches`, `GetSwarmBudget`, `ListSwarmEvidence`
and `GetSwarmIntegration` expose durable records. The current bounded run/dispatch
lists and per-run reads support initial polling; pagination, transport and UI
subscriptions are later work. No unsupported `change_log` event is fabricated.

Callers supply stable request IDs and trusted clocks; newly generated attempt/run
IDs do not override a previous logical operation on retry. Check errors before
using returned values. The scheduler must qualify the exact requested/resolved
selection before claiming; the storage layer alone cannot authorize fallback.

## Verification

Use the pinned local toolchain from [MAC_BASELINE.md](MAC_BASELINE.md), then run:

```bash
cd backend
go test -race -p 1 -timeout 30m ./internal/domain \
  ./internal/service/swarm/qualification ./internal/adapters/agent/commandcode \
  ./internal/storage/sqlite/store ./internal/storage/sqlite -count=1
```

New regressions cover invalid DAGs, two-handle claims, rollback, shared scopes and
budgets, restart/uncertain launch, expired submission recovery, generation fences,
identity/session uniqueness, versioned dependencies and exact evidence bindings.
Migration coverage upgrades existing data, prevents identity rewrites and rolls
back only the additive tables. Live permissions, live model dispatch and complete
desktop swarm workflows still require separate evidence.

The 15-minute full SQLite run was too short on this Mac. All 132 top-level tests
were completed across that run and its exact 14-test remainder; preserve both
receipts. A future single full-package verification needs the longer deadline
above. Do not rerun the completed suites solely to begin SWM-03.
