# Your first Praxis Swarm project: terminal

Build a small duration parser and formatter with an AI team. You set up a
disposable project; a Sol/high lead creates the plan, launches two Luna/low
workers and reviews their combined code. Your existing projects stay separate.

**Before you begin:** this launches real models using your existing Codex login.
Model access and usage depend on your account. These model IDs match the tested
configuration; if unavailable, stop and choose deliberately rather than substitute.
Allow several minutes for the build and model work. Tested on macOS; use Bash
for every command below, in the same terminal. No Node or Electron build is needed.

## 1. Check your tools

<!-- walkthrough: prerequisites -->
```bash
git --version
python3 --version
go version
codex --version
codex login status
```

Use Go **1.27.1** and Python **3.9 or newer**. Confirm Codex reports an active
login. See [official Codex authentication guidance](https://learn.chatgpt.com/docs/auth?surface=cli)
if needed. The daemon must find the working `codex` executable on your `PATH`;
if that name points to a broken wrapper, put the working installation's `bin`
directory first for this shell. Do not change your global model configuration.

## 2. Download and build

Start in a directory where a new `praxis-swarm` folder can be created. Open Bash
first (`bash`), then run:

<!-- walkthrough: build -->
```bash
set -e
git clone https://github.com/sparkplug604/praxis-swarm.git
cd praxis-swarm
export RUN_ROOT="$(mktemp -d /tmp/praxis-first.XXXXXX)"
mkdir "$RUN_ROOT/bin"
go -C backend build -o "$RUN_ROOT/bin/ao" ./cmd/ao
export PATH="$RUN_ROOT/bin:$PATH"
printf 'Keep this path: %s\n' "$RUN_ROOT"
```

The first build may download Go dependencies. Keep `RUN_ROOT`: it holds the
fixture, daemon data and evidence. Nothing is installed globally.

## 3. Create the practice project

The generator creates a committed repository with a specification, tests and two
unfinished helpers. Its initial tests are expected to fail. It starts no models.

<!-- walkthrough: fixture -->
```bash
python3 scripts/swarm/create-supervised-demo.py   --output-dir "$RUN_ROOT/fixture" --project-id first-swarm   --model gpt-5.6-luna --effort low
mkdir "$RUN_ROOT/generator-inputs"
mv "$RUN_ROOT/fixture/"*.json "$RUN_ROOT/generator-inputs/"
cat >> "$RUN_ROOT/fixture/repository/AGENTS.md" <<'EOF'

## Coordinator role
The single-file implementation instructions above apply to workers. The AI lead
may read this repository, write proposals and review evidence to its AO session
artifacts, and use the native AO swarm tool to delegate. The lead must not
implement either helper or edit the frozen source, tests or instructions.
EOF
git -C "$RUN_ROOT/fixture/repository" -c core.hooksPath=/dev/null   -c user.name='Fixture setup' -c user.email=fixture@example.invalid   commit -am 'Clarify lead and worker roles for first project'
```

The generated plan files are moved aside so the lead makes its own plan from
the specification. The role clarification keeps worker instructions from being
mistaken for a ban on lead delegation. The committed source is now frozen.

## 4. Start an isolated daemon

Use a fresh shell without unrelated `AO_*` settings. These settings keep the
trial's AO state separate, disable telemetry and background update checks, and
pick an available local port. A readiness check also verifies the daemon's PID.

<!-- walkthrough: daemon -->
```bash
export AO_DATA_DIR="$RUN_ROOT/data"
export AO_RUN_FILE="$RUN_ROOT/daemon.json"
export AO_PORT="$(python3 - <<'PYPORT'
import socket
with socket.socket() as sock:
    sock.bind(('127.0.0.1', 0))
    print(sock.getsockname()[1])
PYPORT
)"
export AO_HARNESS_UPDATE_CHECKS=off AO_UPDATE_WHILE_RUNNING=off
export AO_TELEMETRY_EVENTS=off AO_TELEMETRY_METRICS=off AO_TELEMETRY_REMOTE=off
export AO_ACP_RUNTIME_DIR="$RUN_ROOT/empty-acp"
mkdir "$AO_ACP_RUNTIME_DIR"
ao daemon >"$RUN_ROOT/daemon.log" 2>&1 &
export AO_DEMO_PID=$!
python3 - <<'PYREADY'
import json, os, time, urllib.request
from pathlib import Path
for attempt in range(120):
    try:
        info = json.loads(Path(os.environ['AO_RUN_FILE']).read_text())
        assert info['pid'] == int(os.environ['AO_DEMO_PID'])
        assert info['port'] == int(os.environ['AO_PORT'])
        with urllib.request.urlopen('http://127.0.0.1:%s/healthz' % info['port'], timeout=1) as response:
            assert response.status == 200
        print('Your isolated daemon is ready.')
        break
    except (OSError, ValueError, AssertionError):
        time.sleep(0.25)
else:
    raise SystemExit('Daemon not ready: inspect RUN_ROOT/daemon.log; do not launch the lead.')
PYREADY
ao project add --id first-swarm --path "$RUN_ROOT/fixture/repository"   --worker-agent codex --orchestrator-agent codex
ao project set-config first-swarm --default-branch main --worker-agent codex
```

## 5. Give the project to your AI lead

This creates the managed lead through the local session API with an explicit
model, reasoning effort and approval mode. The CLI's ordinary spawn command
does not expose all three choices, so this first-run recipe uses the API.

**Running this block starts account usage.** It authorizes one Sol/high lead and
one two-worker run with Luna/low. The lead uses the native AO tool for daemon
operations; its sandboxed shell does not need loopback network access.

<!-- walkthrough: lead -->
```bash
python3 - <<'PYLEAD'
import json, os, sys, urllib.request
from pathlib import Path
root = Path(os.environ['RUN_ROOT'])
info = json.loads(Path(os.environ['AO_RUN_FILE']).read_text())
assert info['pid'] == int(os.environ['AO_DEMO_PID'])
assert info['port'] == int(os.environ['AO_PORT'])
python = str(Path(sys.executable).resolve())
prompt = f"""Use Praxis Swarm for project first-swarm. Read CONTRACT.md, PROGRESS.md
and tests. Create your own concrete proposal from these files; do not read the
prewritten generator JSON outside this repository. Delegate the parser and
formatter to two workers using gpt-5.6-luna, low effort, advisory tokenLimit
600000, and recorded Python tool {python} with version label Python-3.
Use task IDs parser and formatter, with disjoint paths duration_parser.py and
duration_formatter.py. Include exact errors, interfaces and named unittest checks.
Read ao swarm proposal locally for the format if needed. Write proposal.json
in your AO session artifact directory. Use the native AO MCP tool named swarm
(mcp__ao__swarm) for prepare/start/get/stop/review. Do not call shell HTTP or shell
ao swarm daemon commands: shell networking is disabled. Inspect the prepared
summary, then start once with requestId first-project-once and timeoutSeconds 240.
Poll get sparingly. When completed, prepare review, inspect both implementations,
and actually run every returned check argv and Python unittest discovery in the
combined directory. Retain command, cwd, exit code and output in check-results.json
and logs; write review-report.json with runId, combinedDirectory, findings and
artifact paths. Report actual results and remaining issues. Never implement the
helpers yourself, spawn ordinary agents, use subagents, retry or substitute models,
merge or publish. If the native tool is missing, permissions fail, or a run is
uncertain, retain evidence and report the blocker without a replacement run."""
(root / 'lead-task.txt').write_text(prompt)
body = dict(projectId='first-swarm', kind='orchestrator', harness='codex',
            mode='chat', model='gpt-6.1-sol', effort='high', approvalMode='auto',
            branch='swarm/first-project-lead', displayName='First Praxis Swarm lead',
            prompt=prompt)
request = urllib.request.Request(
    'http://127.0.0.1:%s/api/v1/sessions' % info['port'],
    data=json.dumps(body).encode(), headers={'Content-Type': 'application/json'})
with urllib.request.urlopen(request, timeout=150) as response:
    result = json.load(response)
(root / 'lead-session.json').write_text(json.dumps(result, indent=2) + '\n')
lead_id = result['session']['id']
(root / 'lead-id.txt').write_text(lead_id + '\n')
print('Lead started:', lead_id)
PYLEAD
export LEAD_ID="$(cat "$RUN_ROOT/lead-id.txt")"
```

`auto` uses the native approval reviewer with workspace-write; it does not select
unrestricted execution. Workers use workspace-write with on-request approvals.
Account policy or an approval can still block progress. Token reservations are
advisory, not a spending cap.

## 6. Follow the work and find the result

<!-- walkthrough: monitor -->
```bash
ao session get "$LEAD_ID" --transcript 20 --cap 2000
```

Repeat this command occasionally. You should see the lead read the specification,
prepare a proposal, start a run, follow two worker sessions and review their output.
The lead's final response should include its **run ID**, combined review directory,
review report and check-log paths. Use those exact reported paths; they are under
your trial's AO data/session artifacts, not this checkout.

With the reported run ID, use `ao swarm get RUN_ID` to inspect the retained run.
A completed walkthrough has two finished worker turns, a retained supervised
report, a preserved source project, and a lead review with actual check logs.
The supplied fixture has **10 unittest tests**. Read both the test output and
the review decision: passing those tests does not guarantee acceptance.

The lead may report **changes required** after finding an additional defect.
In the terminal verification run, all 10 supplied tests passed, but the lead
caught a parser boundary error and held the candidate for repair. That is a
completed review with an unaccepted candidate, not permission to merge. Keep the
evidence and authorize a separate correction only after inspecting the finding.
See the [actual walkthrough results](evidence/walkthrough-verification.md).

The original helper files remain unfinished: results are in frozen worker
snapshots and the combined review directory. To check that directory yourself,
run `python3 -B -m unittest discover -v` **inside it**. Nothing is merged back.

## 7. Stop the trial and keep the evidence

After the lead reports completion, stop its remaining controller and then your
owned daemon. If stopping a session reports it is already stopped, verify that
state with `ao session get` before proceeding.

<!-- walkthrough: cleanup -->
```bash
ao session exit-agent "$LEAD_ID"
python3 - <<'PYSTOP'
import json, os, urllib.request
from pathlib import Path
info = json.loads(Path(os.environ['AO_RUN_FILE']).read_text())
assert info['pid'] == int(os.environ['AO_DEMO_PID'])
assert info['port'] == int(os.environ['AO_PORT'])
request = urllib.request.Request('http://127.0.0.1:%s/shutdown' % info['port'],
                                data=b'{}', headers={'Content-Type': 'application/json'})
with urllib.request.urlopen(request, timeout=30) as response:
    response.read()
PYSTOP
wait "$AO_DEMO_PID"
test ! -e "$AO_RUN_FILE"
printf 'Evidence retained at: %s\n' "$RUN_ROOT"
```

If a run is still active, use `ao swarm stop RUN_ID` first, then stop the lead.
Ctrl-C ends a CLI wait; it does not cancel the workers. Retain the original run ID
if an outcome is uncertain. Inspect it rather than start again. If readiness
failed before a run file was written, inspect the log and your recorded daemon
PID instead of sending a shutdown request to a different service.

## Common first-run problems

| What you see | What to do |
| --- | --- |
| `codex` fails or login is missing | Fix the executable/login first; do not launch the lead. |
| Requested model is unavailable | Stop and select models explicitly. An empty direct-entry model catalog alone does not establish unavailability. |
| Daemon readiness fails | Read `daemon.log`; check that another process did not claim the selected port. |
| Native `swarm` tool is missing | Use this checkout's binary and a fresh managed `kind=orchestrator` session. |
| Shell loopback permission denied | The managed lead must use the native AO `swarm` tool. Keep its shell sandbox enabled. |
| Run is `stopped` or `needs_attention` | Inspect task errors and controller states. Preserve evidence; no automatic replacement run. |
| Tests fail in the fixture's original folder | Use the combined review directory; source stubs are preserved intentionally. |

Next: [use your own project](GET_STARTED_CODEX.md#use-your-own-project) ·
[Technical reference](LEAD_REFERENCE.md) · [Choose another starting path](LEAD_WORKFLOW.md)
