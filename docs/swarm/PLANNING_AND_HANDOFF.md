# Planning and worker packets

SWM-03 implements a local Go service in `backend/internal/service/swarm/planning`.
It validates a supplied plan, proposes parallel waves, and creates one bounded
worker packet from an existing claimed attempt. It does not call a planner or
worker, launch commands, schedule work, or approve a lesson. The desktop and CLI
are not wired to these APIs yet. [PROGRESS.md](PROGRESS.md) owns current status.

## Call path

1. Supply planner JSON to `ParsePlan`, or `Service.CreateRun` to validate before
   calling the existing transactional store. Invalid input causes no write.
2. Use `ParallelWaves(plan)` for deterministic dependency/resource planning.
   Dependencies finish in earlier waves; conflicting scopes serialize, and
   each wave respects `MaxActive`. This is advice, not runtime admission.
3. The coordinator separately qualifies and claims the task through SWM-01/02.
   Compose requires the resulting run and attempt IDs; callers cannot override
   its contract, worker, budget, tools or entry instructions.
4. Supply the store, trusted `ContextResolver`, `ToolVerifier` and clock to
   `Service.Compose(ctx, runID, attemptID)`. It returns the complete JSON envelope
   and its self-contained Markdown assignment, with hashes and omission receipts.
5. Before resuming, call `ValidateResume(ctx, packet)`. It checks the seal and
   regenerates the packet against current state, context eligibility and tools.
   A valid checksum alone does not make a packet current or authorized.

The existing immutable contract JSON stores the new `work` field. No schema or
SQL changes were needed. Legacy contracts omit `work`, retain their original
bytes/hashes, and remain readable; SWM-03 refuses to compose from them until an
explicit contract revision supplies the required information.

## Plan contract

The schema is defined by `domain.SwarmPlan`, `SwarmTaskContract` and
`SwarmWorkSpec`. Parser tests contain a complete synthetic example. JSON input
is limited to 2 MiB and rejects duplicate/unknown fields, case aliases, trailing
documents, invalid UTF-8, and null scalar coercions. Required fields cannot be
replaced with free-form startup instructions.

Each task's `work` version 1 requires:

- Task type, non-goals, normalized progress path, and an immutable context byte
  limit of 1..1,048,576 bytes. Too-small budgets fail composition.
- Named interfaces with explicit ownership and referenced error rules.
- Error conditions, one signal (`throw`, `return_error`, `reject`), the exact
  error value, destination policy, and mutation/rollback behavior. Every error
  must be referenced by an interface and a named acceptance check.
- An oracle ID plus named argv checks, explicit expected exit codes and expected
  results. Every command's executable must match an exact recorded tool path.
  This specifies acceptance; independent evaluation must still execute the
  protected oracle and demonstrate that it rejects bad submissions.
- A required specification and exactly one required source manifest, each
  addressed by SHA-256. Other required references are permitted.
- Tool names, absolute canonical paths, binary hashes and version labels.
- `onComplete: "submit_and_stop"` and `onBlocked: "report_and_stop"`.

The historical setter ambiguity has a regression fixture: invalid values must
throw `std::invalid_argument` **before mutation**, leaving the destination
unchanged. The tests prove those requirements survive validation and delivery;
they do not claim to execute the game's setter or its protected evaluator.

## Context selection and authority

The resolver is an injected trusted dependency, not an approval database. It
must check current approval/revocation, applicability and provenance through the
source's existing authority. No live Praxis connector or new promotion process
ships here. Tests use clearly synthetic approval identities.

The selector checks returned identity, decision ID/version, content hash, UTF-8
and size. Nonempty task-type, backend and path dimensions AND together;
alternatives within a dimension OR together. Paths are normalized logical
subtrees or terminal `/**` selectors, not filesystem permissions. Optional
priority uses specificity, longest *matching* path, fewer alternatives, then ID.

Required inputs are included whole or composition fails. Optional omissions are
recorded as `not_applicable`, `unavailable`, `unapproved`, `hash_mismatch` or
`budget`. Optional bodies are omitted whole; behavioral guidance is never cut
mid-sentence. Resolution errors do not leak private provider messages.

The budget covers serialized JSON, including the contract, Markdown, included
bodies, omission receipts, byte count and digest. Duplicate representations are
counted conservatively. This is not a tokenizer, a provider context-window
guarantee or evidence of token savings.

## Resume and stop behavior

Only a current, unexpired `reserved` or `running` attempt can compose. Cancelled
runs, old protocols/contracts, changed owners/generations, and known reported
model mismatches fail. A `dispatching` attempt is uncertain and must go through
reconciliation; packet creation cannot authorize a second launch. Unknown
reported-model telemetry remains unknown.

Packets bind the attempt's original base commit, contract hash, operation ID,
resolved worker and capability fingerprint. They do not silently follow a later
integration head. Renewing the same lease or confirming its running session does
not change the assignment. State is checked again after context/tool work.

Generated entry instructions name exactly one task/version/attempt and its stop
conditions. Altered or resealed startup instructions fail canonical comparison.
Structured contradictory fields are rejected; this is not a general detector
of contradictions or prompt injection in arbitrary natural-language references.
Reference prose and old progress documents cannot override canonical assignment
identity or grant execution permissions.

`FileToolVerifier` checks canonical paths and executable regular-file bytes
without launching them. Nonblocking/no-follow open rejects FIFO and symlink
substitutions; identity is checked again after reading. The version label is
coordinator-provided metadata bound to the hash, not independently executed
version output. Subprocesses, scripts, dynamic libraries and environments still
need qualification and containment in the runtime layer.

For a legitimate tool change, the coordinator must settle the old attempt,
revise the immutable contract, qualify and claim a new attempt, then generate a
new packet. There is no automatic migration or silent fallback. Closing a run
does not itself prove that a real process stopped; SWM-02's trusted-caller rules
still apply.

Composition is a read-only check, not a transaction spanning filesystem/provider
state or a dispatch permit. State and eligibility can change after it returns;
the eventual scheduler must revalidate and use fenced admission at execution.
Real source-manifest verification, filesystem alias isolation, provider quotas,
live parallel workers, protected execution and measured savings remain later
work packages.

## Focused verification

With the pinned tool environment from [Mac setup](MAC_BASELINE.md):

```bash
GOMAXPROCS=2 go test -C backend -race -p 1 ./internal/service/swarm/planning ./internal/domain
```

Coverage includes strict plans, deterministic/conflict-aware waves, synthetic
approval/hash failures, byte limits, single-assignment delivery, tampering,
revocation, changed tools, mid-composition cancellation/expiry, and SQLite
restart plus explicit contract migration. This is automated packet inspection,
not a live worker usability or savings trial. Receipts are linked from progress.
