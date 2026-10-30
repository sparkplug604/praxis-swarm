# Reproduce the Apple Silicon Mac baseline

Read [PROGRESS.md](PROGRESS.md) for actual results and unresolved gates. These
commands prepare the public local code; they do not start agents or qualify a swarm.

Prerequisites: Apple Silicon macOS, Xcode command-line tools, Git, Python 3,
curl, network access for pinned dependencies, and ample free disk space. Cold
native builds, test binaries and caches consume several GiB; leave approximately
10 GiB available and run the heavy checks sequentially on a memory-limited Mac.

From the repository root, in Bash:

```bash
# Optional: point this at an existing SWM-00 toolchain directory.
# export SWARM_RUNTIME_ROOT=/absolute/path/to/swarm-runtime
bash scripts/swarm/bootstrap-macos.sh
source scripts/swarm/env.sh
npm ci --ignore-scripts
npm --prefix packages/product-ui ci --ignore-scripts
npm --prefix frontend ci --ignore-scripts
bash scripts/check-public-boundary.sh
```

The bootstrap pins Go 1.27.1, Node 24.22.2 and npm 10.9.9. Go and Node archives
are SHA-256 checked against the official release metadata recorded in SWM-00.
npm package installation uses npm integrity verification. No global tool version
is changed. Lockfiles must remain unchanged. The private cloud submodule is not needed.

Prepare Electron before anything imports its Node package; that package otherwise
tries to download Electron on demand. Then prepare SQLite for **Node** unit tests:

```bash
(cd frontend && node node_modules/electron/install.js)
(cd frontend && npm rebuild better-sqlite3 --foreground-scripts)
bash scripts/swarm/run-check.sh backend-build
bash scripts/swarm/run-check.sh frontend-typecheck
bash scripts/swarm/run-check.sh frontend-tests
bash scripts/swarm/run-check.sh backend-race
```

The check runner removes inherited `AO_*` application settings from its child
environment; a lab redirect override otherwise changes authentication test expectations.
Do not run the heavy suites concurrently. Do not run tests against a live lab
desktop. A failed or interrupted suite is not a pass; retain its complete log,
then reproduce failures independently before blaming or changing upstream code.

For stall diagnosis, preserve module progress with the optional local reporter:

```bash
bash scripts/swarm/run-check.sh frontend-tests --reporter=default \
  --reporter=../scripts/swarm/vitest-progress.mjs
```

Set `SWARM_TEST_TRACE_CASES=1` to include individual test starts. This reporter
does not impose a suite deadline; the caller must bound the diagnostic run and
record interruptions separately from test failures. SWM-00 exercised the reporter
on one file only; it did not resolve or rerun the stalled full suite.

After tests, build for the **Electron** ABI and launch:

```bash
(cd frontend && node scripts/build-daemon.mjs --dev)
(cd frontend && node scripts/build-browser-extension.mjs)
(cd frontend && node node_modules/@electron/rebuild/lib/cli.js --force --only better-sqlite3)
export SWARM_LAB_ROOT="$(mktemp -d /tmp/praxis-swm00.XXXXXX)"
bash scripts/swarm/launch-local.sh
```

The pinned Electron rebuild tool hardcodes `~/.electron-gyp` and
`~/.electron-rebuild-cache`; allow these compiler caches when using a filesystem
sandbox. They are separate from application state. Rebuild SQLite for Node again
before a subsequent Vitest run. The launch runs the normal Electron/Chromium
sandbox; nesting `sandbox-exec` caused Chromium subprocess failures and was rejected.

Keep `SWARM_LAB_ROOT` short: macOS Unix-domain socket paths have a small length
limit. The launcher rejects data roots over 70 characters. The launch process
sets all three required paths: `AO_DATA_DIR`, `AO_RUN_FILE` and
`AO_DEV_ELECTRON_DIR`. `AO_DATA_DIR` alone does not isolate upstream development
state. The launcher pins `AO_DEV_DAEMON_BINARY` to this checkout's validated daemon,
overriding any inherited development binary. Preserve the scratch path if you
need to inspect its database afterward.

The launch disables remote telemetry and harness update checks and bypasses
optional ACP/browser/cloudflared preparation. It is **not an offline sandbox**:
upstream startup probes installed tools/authentication and can contact services.
The desktop's install buttons can invoke Homebrew and alter host software. Do not
activate installers or start agents as part of the empty-board verification. The
SWM-00 evidence separately records an observed tmux Homebrew installation.

Verify the window is the actual Orchestrator development UI (not Electron's
default welcome window), and verify the scratch daemon:

```bash
curl --fail "http://127.0.0.1:${SWARM_LOCAL_PORT:-43187}/healthz"
curl --fail "http://127.0.0.1:${SWARM_LOCAL_PORT:-43187}/api/v1/sessions"
```

Expected: `status: ok`, executable path in this checkout, working directory in the
short scratch root, and an empty sessions array. Quit the app and verify the owned
daemon exits. Do not use `ao start`, publish, sign, notarize, or run packaging for
this baseline. The omitted runtimes and model adapters remain unqualified.
