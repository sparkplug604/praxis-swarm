# Your first Praxis Swarm project: ask Codex

Ask Codex to handle the local setup and run a small AI-team project for you.
You describe the goal and choose models; the managed lead creates the plan,
delegates implementation and tests the result.

## Before you paste the request

Open a local Codex chat with access to a working folder. You need Git, Python 3.9+,
Go 1.27.1 and a working Codex CLI with your existing login. Codex can check these
first. This walkthrough was tested on macOS; it starts real model sessions and
uses account credits. It does not require the Praxis Swarm desktop application.

The Codex chat you are talking to handles setup. It launches a separate
**AO-managed Sol/high lead** with native Praxis Swarm tools. That lead coordinates
**two Luna/low workers**. Simply pasting a swarm request into an ordinary chat
does not automatically attach those native tools.

## Copy this request into Codex

```text
Set up and run my first Praxis Swarm project locally.

Use https://github.com/sparkplug604/praxis-swarm. Read
 docs/swarm/GET_STARTED_TERMINAL.md and follow its tested setup, session creation,
 native-tool workflow and cleanup. Preserve all existing work and use a fresh
 disposable duration parser/formatter fixture and isolated daemon data.

Use gpt-6.1-sol with high reasoning as the managed AI lead and gpt-5.6-luna with
 low reasoning as both workers. I authorize this one lead and one two-worker run
 using my existing Codex login. Check prerequisites before launching models;
 do not create an account, change global settings or substitute models.

Have the managed lead read the fixture specification and tests, create its own
 proposal (not use the generator's prewritten plan), delegate the two helper
 files, and actually inspect and test the combined result. The setup chat and
 lead must not implement the helpers themselves. Use the native AO swarm tool
 for managed-lead daemon calls; keep workspace-write protections enabled.

Show me the lead session ID, run ID, actual test results, review-report path and
 artifact location. Confirm that the original fixture was preserved. Stop only
 this trial's sessions/daemon when finished and retain its evidence. If anything
 is blocked or uncertain, report it without a replacement run. Do not merge,
 publish or alter my other projects.
```

Codex may request a necessary local execution or network permission. Read the
request and approve only the work above. A prerequisite or permission failure
should be reported before model execution continues.

## What you should see

Codex first reports its disposable project and lead session ID. The managed lead
then creates a proposal, starts the two workers and reviews the assembled result.
At completion, ask for a concise summary if it was not already provided:

> Show the run ID, each worker's changed file, the checks actually run by the lead,
> any defects, and where I can inspect the combined code and review logs.

The fixture supplies **10 tests**. A completed workflow reports their actual
results, two finished worker turns, the preserved original repository, and the
lead's acceptance decision. The lead can mark **changes required** even when
those tests pass: review can uncover cases the supplied tests missed.

Read the findings before deciding on a correction or integration. The candidate
stays separate and is not automatically merged. Keep the trial path so you can
inspect the code and evidence later. See the [tested outcomes](evidence/walkthrough-verification.md).

## Use your own project

After the practice run, provide Codex with your repository path and one concrete
change you want. For example:

> Use Praxis Swarm in `/absolute/path/to/my-project`. Use Sol/high as the lead and
> Luna/low as both workers. Read the existing requirements and tests; propose two
> independent assignments, run them, then inspect and test the combined result.
> Preserve my existing changes, retain the evidence, and report before integration.

The current supervised profile requires a **clean committed source baseline** and
**two independent assignments with disjoint file scopes**, using one worker model
and effort. If there is uncommitted work, Codex should preserve it and establish
an agreed isolated baseline; it should not discard it or commit it without your
instruction. If the work cannot sensibly fit two independent assignments, the
lead should explain the dependency and help choose a suitable first task.

The builder validates a concrete proposal; it does not decide product requirements
for you. Supply the desired behavior, existing interfaces and acceptance criteria,
or let the lead ask about the missing decisions before worker execution.

[Terminal commands](GET_STARTED_TERMINAL.md) · [Technical reference](LEAD_REFERENCE.md) ·
[Walkthrough test results](evidence/walkthrough-verification.md)
