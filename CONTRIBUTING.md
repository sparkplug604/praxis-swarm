# Contributing to Praxis Swarm

Praxis Swarm is an independent fork of Agent Orchestrator. Contributions here
improve this fork's supplied-plan workflow, handoff contracts, evidence, examples
and documentation. The process below applies to this fork; upstream contributions
follow upstream's own contributor guide.

Start with a small, reproducible bug, an unclear setup step, or a missing boundary
case. Search [this fork's issues](https://github.com/sparkplug604/praxis-swarm/issues)
and linked pull requests before duplicating work. Small fixes and documentation
improvements can be proposed directly. For a larger feature or changed runtime
contract, open an issue describing the problem and proposed scope before investing
in an implementation. Review and response times are not guaranteed.

## Make one useful change

1. Read [README.md](README.md) for the current product boundary and
   [PROGRESS.md](docs/swarm/PROGRESS.md) for verified status.
2. Follow [AGENTS.md](AGENTS.md) for architecture, generated files and validation.
   This guide describes contributions to Praxis Swarm; inherited upstream release
   and distribution notes do not mean this fork ships those products.
3. Branch from the fork's `main`, keep the change focused, and preserve unrelated
   work. For an existing issue, leave a brief note about your intended change so
   others can coordinate.
4. Run checks relevant to the change and record their actual results. Distinguish
   passed, failed, skipped and not-run checks; include any environment limitation.
5. Open a pull request to [Praxis Swarm](https://github.com/sparkplug604/praxis-swarm/pulls)
   using the supplied template. Explain the concrete behavior before and after,
   link an issue when applicable, and update docs for changed user workflows.

| Work | Start here | Relevant verification |
| --- | --- | --- |
| First-run docs or example | `README.md`, `docs/swarm/`, `scripts/swarm/` | Check paths, commands and expected outcomes against source; label commands you did not run |
| Planning, handoffs or supervised lifecycle | `backend/internal/service/swarm/` | Focused Go tests, with fakes for provider behavior and race checks for concurrency |
| CLI or HTTP behavior | `backend/internal/cli/`, `backend/internal/httpd/` | Request/error tests and API route/schema checks |
| Broader AO behavior | [architecture](docs/architecture.md), [development guide](docs/development.md) | The relevant inherited package and platform checks |

## Set up and verify

The smallest entrypoint uses Bash, Git, Python 3 and Go 1.27.1:

```bash
bash scripts/swarm/demo-offline.sh
```

It launches no models and writes a report outside the checkout. See the
[offline quick start](docs/swarm/QUICKSTART.md) for dependency-download behavior,
expected results and failure logs. Node, Electron and a model account are not
required for this demo. The inherited frontend and desktop require their own
[development setup](docs/development.md); use checkout-local dependencies.

For a supervised-service change, a focused starting check is:

```bash
GOMAXPROCS=2 go -C backend test -race -p 1 ./internal/service/swarm/supervised
```

Choose checks for the code you changed; this example is not a substitute for the
full applicable CI requirements in AGENTS.md. Keep known baseline failures
separate from new regressions. For API changes, edit the Go DTOs and operation
registry, run `npm run api`, and include both generated OpenAPI and TypeScript
schemas. Do not hand-edit generated storage or API files.

Use disposable projects, worktrees and AO data for diagnostics. Do not use tests
to launch a model, change account configuration or consume paid usage without
explicit authorization. A live trial must declare its model, scope, account-use
boundary and evidence plan; provider calls are not a routine CI prerequisite.
Preserve uncertain launch/stop records and user work. Do not turn a failed probe
into proof that a process stopped, or use cleanup to conceal a failed run.

## Reports and proposals

Use [this fork's issue forms](https://github.com/sparkplug604/praxis-swarm/issues/new/choose).
A useful bug report says what you did and what happened. When available, include
the checkout commit, OS, command, expected result and whether the run used test
doubles or real models. A diagnosis is optional. Describe intermittent failures
even when you cannot reproduce them reliably.

Share small, relevant evidence excerpts. Remove credentials, personal paths and
unrelated task content before posting logs. Keep observed facts separate from an
agent's interpretation. Agent-assisted contributions are welcome, but review the
diff and evidence yourself; generated claims are not verification.

Feature requests should describe the user's problem and a concrete outcome.
Do not present planned functionality, a helper-process test, or one successful
model run as a broadly qualified capability. Acceptance, production integration,
spending limits and containment need their own evidence.

Be respectful and specific in issues and reviews. This fork currently coordinates
through GitHub issues and pull requests; no separate community or support service
is promised. Upstream-specific contributions should follow the
[upstream contributor process](https://github.com/OrchestratorInc/agent-orchestrator/blob/71bb19f5947a64853ee6d11225bc896563878c50/CONTRIBUTING.md).

Modification notice: adapted from Agent Orchestrator's contributor guide for
Praxis Swarm. Upstream recruiting, community, approval and hosted-service
commitments were replaced with this fork's scope and contribution process.
Original attribution and history are retained; see [UPSTREAM.md](docs/swarm/UPSTREAM.md).
