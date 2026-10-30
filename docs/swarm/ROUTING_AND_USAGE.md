# Routing and usage admission

SWM-04 implements local backend contracts and SQLite accounting. It does not
launch models, collect provider quotas, expose a swarm UI, or demonstrate token
savings. See [progress](PROGRESS.md) for verified checks and the next gate.

## Coordinator workflow

1. Create the SWM-02 budget and run. Before its first attempt, call
   `ConfigureSwarmRouting` with one immutable policy for the canonical external
   account. Total and premium token limits are separate; premium is a subset of
   total usage. Duplicate policy submissions are idempotent. A second bucket for
   the same account is rejected, preventing parallel runs from resetting limits.
2. Call `UpdateSwarmQuota` with an identified, dated snapshot. Identical snapshots
   replay; replacements require the next revision and a later observation.
   Include explicit model-meter scopes and any account-wide `*` windows.
3. Call `routing.Service.Admit` with the frozen contract version, logical request
   ID, capability fingerprint, catalog scope, context-token estimate and lease.
   New requests refresh AO's existing model catalog, calculate route advice, then
   atomically claim the task and reserve usage through the existing store.
4. The future scheduler must independently qualify the actual launch, bind its
   workspace/model/permissions and use the durable dispatch operation. Admission
   alone grants no launch permission. Apply `routing.SwarmSpawnConfig` before
   passing configuration into AO's session manager.
5. Record immutable response facts through `RecordSwarmUsageReceipt`, including
   the attempt, dispatch operation, provider and response IDs. Supply unknown
   counters as nil, never invented zeros. Failed, cancelled and review work also
   consumes capacity until authoritative final usage is settled.
6. Settle the authoritative attempt total using `SettleSwarmUsage`. Read
   `GetSwarmUsageReport` for routed admission and presentation; the older
   `GetSwarmBudget` exposes nominal SWM-02 reservations and does not include late
   receipt holds. Do not sum `Observed` and `Settled`: they describe overlapping
   evidence, not independent charges.

Implementation: `backend/internal/service/swarm/routing`,
`backend/internal/domain/swarm_routing.go`, and
`backend/internal/storage/sqlite/store/swarm_routing.go`.

## Selection and quota rules

`RecommendRoute(task, catalog, quota, policy, now)` is pure. It validates explicit
model/effort requests against both the catalog and policy. Defaults may shift or
clamp effort within provider-advertised ordering and policy bounds. Explicit
requests are never silently substituted, clamped or upgraded. Spare quota never
causes a premium upgrade. Admission uses the selection already frozen in the
contract; planning may use default advice before freezing that contract.

Catalog identity, supported effort, context capacity, authentication warnings,
staleness and runnable membership are checked. Catalog validation is not a live
authentication or permission proof. Unknown context capacity rejects admission;
the supplied context-token estimate is not a measured tokenizer result. SWM-03's
packet byte budget remains a separate constraint.

Quota is scoped: exhausting a premium meter does not exhaust an independent
worker meter. Account-wide windows constrain both. A missing exact meter remains
unknown even if an account-wide window exists. Missing token capacity or usage
also remains unknown; percent-only observations are not fabricated token counts.
Where capacity is supplied, percentage usage imposes a conservative lower bound.
Known constraints are enforced even when other observations are missing.

Unknown quota rejects admission unless policy explicitly allows it; that choice
still enforces known constraints and local budgets. An expired snapshot or reset
timestamp always requires refresh. Neither a reset nor cancellation sets usage
to zero. Snapshot usage plus local settled usage, active commitments and the new
request must fit the applicable known allowance, using overflow-safe arithmetic.

## Replay and accounting

Admission recovers an existing logical request before catalog discovery. Retries
must match its run, task, version, capability, context estimate and catalog scope.
They retain the original attempt, route hashes and operation without reserving
again. `RouteAdvice.Replayed` identifies recovered advice; its quota is explicitly
unknown/currently unverified. A false value does not prove that a caller created
the attempt: indistinguishable simultaneous retries may receive the same result.
The durable dispatch protocol, not this flag, must enforce launch ownership.

Receipt ID and `(bucket, provider, response)` identity deduplicate delivery.
Identical delivery under another receipt ID is harmless; conflicting content or
attribution is rejected. Input plus output are the token total. Cache and
reasoning counts are subsets, not extra charges. Missing counts and cost remain
unknown in stored evidence. Premium classification reflects the recorded route;
it is not proof of the provider's actual billing model.

For open attempts, admission holds the larger of the reservation and known
observed usage. After final settlement, a late receipt exceeding that total is
retained and the excess is held against future admission. Replaying the original
settlement remains idempotent and does not clear the hold. Runtime or receipt
model mismatches block new admissions for that account rather than silently
reclassifying its usage. Reports expose unknown responses and unresolved attempts
alongside independent total and premium projections.

## Cosmetic calls

`SwarmSpawnConfig` sets `DisableCosmeticModelCalls` and clears `NameFromPrompt`.
The explicit flag is respected by delegation's Git/scratch title refinement,
shared session spawning, and direct session-manager title generation. Existing
non-swarm defaults are preserved. This suppresses those optional naming calls;
it is not a global network or model-call prohibition.

## Trust boundaries and remaining work

- These are trusted coordinator APIs, not an authorization boundary. The raw
  store validates frozen identities, policy and atomic accounting; it does not
  independently query a provider catalog. Future dispatch must use the service
  and live qualification, not construct arbitrary store bindings.
- The coordinator supplies canonical account identity and provider observations.
  Credential-to-account verification, receipt authentication and live collection
  are not implemented. Distinct aliases for one account cannot be detected here.
- Provider usage can already include local spend. Until a reconciled watermark
  exists, counting both is deliberately conservative and may block useful work.
  External usage after a snapshot can also change actual headroom; local
  reservations are admission controls, not provider-enforced spending caps.
- Policy and budget rollover are not implemented. Do not create another account
  identity to bypass commitments. Policy changes, reset-period reconciliation,
  late-overrun resolution and mismatch recovery need explicit, auditable support
  preserving prior attribution and unresolved usage.
- SWM-01 live qualification and SWM-05 scheduling/workspace ownership remain
  required before SWM-06's live parallel demonstration. These backend tests do
  not qualify the old Spark adapter or prove a live product swarm.

The additive migration and generated queries use AO's existing SQLite database.
Legacy contracts omit new optional role/routing fields, preserving historical
serialized bytes. No global Praxis database, game source, or prior trial evidence
was changed. Adapted concepts and source licenses are recorded in
[provenance](UPSTREAM.md).
