# Praxis Swarm

**An AI project lead that puts the right agents to work.**

Praxis Swarm is being built to help you turn a goal into coordinated work across a team of AI agents and models.

The vision is an AI project lead that can:

- Develop a plan from your goal.
- Choose suitable agents and models for different assignments.
- Coordinate work across the team.
- Review results, identify issues, and help bring the work together.
- Keep you informed and involve you when your judgment is needed.

Today, Praxis Swarm provides an early foundation for that experience. Its supervised alpha takes a prepared plan, gives two workers structured assignments, runs them in parallel, and preserves their results for review.

[Get started](https://github.com/sparkplug604/praxis-swarm/blob/main/docs/swarm/SUPERVISED_WORKFLOW.md) · [See the demonstration](https://github.com/sparkplug604/praxis-swarm/blob/main/docs/swarm/SUPERVISED_WORKFLOW_RESULT.md) · [Project progress](https://github.com/sparkplug604/praxis-swarm/blob/main/docs/swarm/PROGRESS.md)

## Why Praxis Swarm?

A useful AI team needs more than multiple agents working at once. Each worker needs to understand its assignment, have the context to carry it out, and produce results that can be brought back to the project.

Praxis Swarm gives that work a structure: clear handoffs, separate workspaces, supervised execution, and results that stay connected to the original assignments.

Praxis Swarm builds on [Agent Orchestrator](https://github.com/OrchestratorInc/agent-orchestrator), using its agent sessions and worktrees as the foundation for this workflow.

## From plan to coordinated work

Once you have a plan, Praxis Swarm helps put it into action. It prepares an assignment for each worker, starts the workers in separate workspaces, and keeps their results together for review.

In the current alpha, you submit the prepared plan to Praxis Swarm, which runs two workers in parallel.

### Prepare clear handoffs

Praxis Swarm turns each assignment into an **assignment packet**: a structured brief that gives the worker information it needs to get started.

A packet brings together:

- **The job:** What the worker is being asked to accomplish.
- **Its responsibilities:** The files or areas assigned to it, along with work outside its scope.
- **How the pieces fit:** Interfaces, expected behavior, and error rules relevant to the assignment.
- **The context:** Supplied specifications and reference material selected for the task.
- **The expected result:** Acceptance criteria, recorded tools, and instructions for reporting results or blockers.

For example, a plan might assign one worker to build a parser and another to build a formatter. Each receives its own packet, with the information needed to make the two components work together.

Praxis Swarm checks supplied context against the content identified in the plan and records when optional context is omitted. The packet stays with the run record, so you can review **what a worker was given alongside what it produced**.

### Start from a shared baseline

Praxis Swarm checks the plan and source before starting the workers. Each worker receives a separate worktree created from the source commit specified in the plan.

This gives the workers a shared starting point and keeps their changes separately reviewable.

### Follow the run

Praxis Swarm records the worker launch intents and supervises the run through a deadline. You can inspect the run’s status, retrieve its record, or request a stop.

When an outcome needs attention, Praxis Swarm keeps that visible in the run record. If a response is uncertain, you can retrieve the recorded request using the same request ID and input.

### Review the results

Praxis Swarm captures the workers’ candidate files and keeps them alongside the run information. The record can include:

- The plan and worker assignment packets.
- Source commit and snapshot information.
- Candidate files, content hashes, and tracked-file patches.
- Worker status and available execution observations.
- Available model, timing, and usage information.
- Errors, limits, and context omissions.

This gives you a way to see **what each worker was asked to do, what it produced, and what Praxis Swarm observed during the run**. You can then inspect and test the captured work before integrating it.

## Praxis Swarm in action

In a supervised demonstration, Praxis Swarm ran two Luna/low workers on a parser-and-formatter assignment. The workers operated in separate worktrees, and their provider turns overlapped.

Praxis Swarm captured their results, and the combined frozen implementation was independently tested on Python 3.9 and 3.14.

[Read the demonstration results](https://github.com/sparkplug604/praxis-swarm/blob/main/docs/swarm/SUPERVISED_WORKFLOW_RESULT.md)

## What works today

Praxis Swarm is a **supervised source alpha**. Its current workflow supports:

| Capability | Available today |
|---|---|
| **Planning input** | A prepared plan with separate assignments for two workers |
| **Workers** | Two parallel Codex workers using a selected model and reasoning effort |
| **Handoffs** | Structured assignment packets with relevant supplied context |
| **Workspaces** | Separate worktrees based on the plan’s source commit |
| **Supervision** | Launch records, deadlines, status inspection, and explicit stop |
| **Results** | Captured candidate files, hashes, patches, and available execution observations |

You submit the prepared plan and review the results. The live workflow has been demonstrated on one Apple Silicon Mac.

## Get started

- [Run a supervised two-worker swarm](https://github.com/sparkplug604/praxis-swarm/blob/main/docs/swarm/SUPERVISED_WORKFLOW.md)
- [Explore the offline coordination demo](https://github.com/sparkplug604/praxis-swarm/blob/main/docs/swarm/QUICKSTART.md)
- [Read the release notes](https://github.com/sparkplug604/praxis-swarm/blob/main/docs/swarm/RELEASE_NOTES.md)
- [Follow project progress](https://github.com/sparkplug604/praxis-swarm/blob/main/docs/swarm/PROGRESS.md)
- [Contribute](https://github.com/sparkplug604/praxis-swarm/blob/main/CONTRIBUTING.md)
