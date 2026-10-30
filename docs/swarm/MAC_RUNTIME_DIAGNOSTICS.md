# Mac runtime diagnostics

Status: **offline helper-process diagnostics implemented; live qualification is
not passed**. The native detached-child test deliberately reproduces a failed
containment requirement. A passing regression test means that failure is detected
and reported as unknown; it does not mean the runtime contains every descendant.
Follow [PROGRESS.md](PROGRESS.md) for the next gate.

## What runs

The call path is `macruntime.Sandbox.Wrap` → `conpty.PinnedRuntime.Create` → AO's
shared Unix host spawner → pinned executable `pty-host` → native Mac PTY →
`/usr/bin/sandbox-exec` → disposable helper. There is no second production
subprocess launcher, model request, provider substitution or ordinary AO session
cleanup. The existing scheduler/exact receipt bridge is not wired to this policy
yet; command wrapping must happen **before** qualification/final receipt hashing.

`NewPinnedRuntime` binds one canonical executable and SHA-256, one disposable
registry directory and `explicit-full-env-v1` behavior. Only one binding is
allowed per coordinator process because the inherited registry scope is global.
The environment is complete, not an overlay on the user's shell. `DYLD_*` and
`LD_*` loader overrides reject before the host starts. Direct absolute argv and
immediate start are required; the pinned path has no hybrid runtime fallback.
Post-start failure preserves possible effects and registry evidence. Diagnostic
`Destroy` rejects recovered/stale handles; generation-fenced stop can recover
through an exact host acknowledgement. Started session IDs cannot be reused.
The binding includes stop protocol version 1.

`NewSandbox` receives that pinned runtime. It binds the policy, host, sandbox
executable, explicit tools and physical root identities. Workspace, scratch,
coordinator, evaluator and actual runtime registry directories must be disjoint,
including case/firmlink aliases. Exposed trees reject pre-existing symlinks,
hardlinks and special files; these checks repeat before wrapping. Paths with
control characters reject. Policy text is passed as an argument, not loaded from
a worker-writable file.

The offline policy permits system-library/tool reads, its workspace, private
scratch and `/dev/null`. Worker workspace writes are enabled; reviewer workspace
writes are denied. Coordinator/evaluator/registry access and network access are
denied. File metadata is otherwise readable for traversal; this is not metadata
confidentiality. It does not grant credentials, provider access or arbitrary
build-tool access. The host itself remains outside the worker sandbox.

## Evidence and reproduction

Initial evidence (local archive: `outputs/swarm-build/swm-05-mac-runtime`; not distributed) and
fenced-stop evidence (local archive: `outputs/swarm-build/swm-05-fenced-stop`; not distributed) contain
native test logs, initial failures, final source hashes and preservation checks.
Tests use real processes and real files on macOS 26.6.2 (25G83), Darwin arm64.
They do not use a model or turn test evidence into a live permission receipt.

From this repository, using the existing local tool environment:

```bash
export SWARM_RUNTIME_ROOT="$PWD/../work/swarm-runtime"
source scripts/swarm/env.sh
GOMAXPROCS=2 go test -C backend -race -p 1 -timeout 100s \
  ./internal/adapters/runtime/conpty/... \
  ./internal/service/swarm/macruntime -count=1 -v
GOMAXPROCS=2 go test -C backend -race -p 1 \
  ./internal/service/swarm/scheduler -count=1
```

An outer sandbox may deny PTY host loopback binding or nesting `sandbox-exec`.
That failure must stay visible. The recorded native run used authorized execution
outside that outer sandbox with disposable fixtures, then applied the worker
sandbox itself. Do not disable the worker sandbox to make a test pass.

Worker/reviewer tests each check 23 filesystem/network behaviors, including
existing-file overwrite, child-shell writes, protected deletion/rename/link
attempts, real case/firmlink aliases, and quote/backslash/Unicode paths. Direct
denial assertions require `EPERM`/`EACCES`; aliases are independently established
as the same inode and child shells must write readiness/result markers. Original
protected bytes are checked afterward. This prevents missing files or failed
child startup from masquerading as sandbox enforcement.

The detached helper starts another session with `setsid`, ignores HUP/TERM and
survives an acknowledged generation-fenced PTY stop. Its protected write still
fails and the host replays the same stop receipt. The pinned probe reports
unknown, and the diagnostic separately kills and verifies absence of its known
fixture PID. That fixture cleanup is not a product cancellation implementation.

Native close tests also cover stubborn original-group descendants. After an
observed leader exit, the connection never signals its retained numeric group:
confirmed absence allows close, otherwise it preserves uncertainty across repeat
calls. This avoids treating an old group number as enduring ownership. A native
fixture proves that parent-first descendants can remain and cleans them itself.

## Generation-fenced stop and cancellation

`PinnedRuntime.StopFencedRuntime(ref)` is a separate runtime port. A complete
retained registry entry supplies a loopback route, not authority: the host checks
the session and immutable `AO_RUNTIME_LAUNCH_ID` captured before worker startup.
Wrong-generation requests, even with forged registry hints, cannot close the
worker's PTY. Pinned hosts also refuse legacy kill messages. The client bounds
payloads, reads and deadlines; old hosts, disconnects and lost replies remain
uncertain. There is no automatic retry or fallback kill.

The host closes its PTY once across stop/replay/shutdown, retains the result and
returns cleanup errors. Its listener and registry survive a stop for explicit
recovery. This receipt lives in the host, not a new persistent stop table; host
death or an unavailable receipt leaves uncertainty. An acknowledgement proves
the exact host accepted the stop and reports its PTY cleanup result. It does
**not** prove descendant absence or authenticate against another trusted host
actor who can forge the local transport. Diagnostic `Destroy` may separately
dispose only an original owned host; that path is not scheduler cancellation.

`scheduler.Service.Cancel` and `ExactRuntime.Stop` require an already cancelled
durable run, an unexpired current lease and matching immutable execution
receipt/result. Authority is checked again immediately before external stop.
Lease checks and I/O are not atomic: safety during takeover relies on irreversible
cancellation and the same launch generation `SwarmHash(OperationID)`. The current
lease still fences every resulting state write. Every acknowledged stop triggers
observation; mismatches, failures and unknown absence keep capacity reserved.
Missing/pending or multiple-handle outcomes cannot become absence. A final
single-use `no_effect` result can finalize without stop. Reserved attempts and
full-run cancellation orchestration remain outside this API.

## Next containment diagnostic

Use a **per-attempt Linux container inside a pinned Docker Desktop guest** as the
next offline candidate for Linux-compatible work. Local read-only inspection
found Docker CLI 28.3.2 and Docker Desktop's bundled backend/QEMU. It did not
start/probe the engine, inspect/pull an image or launch a container/VM. Engine,
image and guest readiness are unknown; installation alone proves no isolation.
Mac-only workloads must retain their platform requirement. A dedicated VM is a
stronger possible boundary but is not provisioned or qualified here.

Implement/qualify this candidate in this order, reusing the current runtime ports
and execution receipts rather than adding a parallel task queue:

1. Inventory engine/guest/image state read-only. Pin engine and guest identity,
   immutable image digest and exact launch configuration before any diagnostic.
   Missing prerequisites become explicit setup work, not implicit downloads or
   daemon starts.
2. Bind an immutable container ID plus launch generation to each attempt. Use a
   private PID namespace and nondelegated cgroup, disabled restart, minimal
   capabilities, explicit filesystem mounts and offline networking. Do not expose
   the Docker socket, host processes, privileged mode or host-exec services to
   workers. Prove reviewer mount denial and protected coordinator/evaluator/
   registry/credential denial separately.
3. Exercise double-fork/`setsid`, parent-first exit, ignored signals and concurrent
   spawning. Destroy the isolation domain and establish it is quiescent and
   cannot restart. `docker wait`, names and process-list snapshots alone are
   insufficient absence evidence. Retain exact-ID evidence before cleanup.
4. Exercise lost replies, coordinator/engine restarts, stale IDs and replacement
   attempts. Cancellation/recovery must never stop a replacement or release an
   unresolved reservation. An engine-level uncertainty remains unknown.
5. Only after these offline checks pass, connect the qualified boundary to the
   exact launcher. Bind the final wrapped command, complete environment and
   policy before authorization; define stable permission receipt renewal. Then
   qualify a provider before daemon/UI dispatch or SWM-06's model demonstration.

Until a boundary passes, the native Mac probe continues to withhold terminal
absence. No live release, automatic fallback or model demonstration is implied.

`sandbox-exec` uses a private Apple interface and this policy is intentionally
narrow. TOCTOU against a concurrent trusted host actor, arbitrary inherited
descriptors, the unauthenticated local host transport, credential/network-enabled
providers and production toolchains remain outside this result. Do not describe
this as a universal same-user adversarial sandbox or a completed swarm product.
