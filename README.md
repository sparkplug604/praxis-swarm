# Praxis Swarm

**An AI project lead that puts the right agents to work.**

Praxis Swarm connects an AI project lead to a team of coding agents. Describe a goal and ask the lead to use Praxis Swarm: the lead prepares a plan, delegates work, follows the workers, and reviews their results.

For an incomplete goal, [Project Discovery](docs/swarm/PROJECT_DISCOVERY.md) lets the managed lead ask questions and retain your decisions in a versioned brief before planning.

The current supervised alpha has demonstrated this complete workflow with **Sol/high as the lead and Luna/low as both workers**, as well as a team using Luna/low throughout.

Our goal is to make coordinating an AI team feel more like working with a project lead: you set the direction, and the lead helps organize the work and bring the results together.

[Get started](docs/swarm/LEAD_WORKFLOW.md) · [See the demonstration](docs/swarm/evidence/lead-sol-luna-confirmation.json) · [Project progress](docs/swarm/PROGRESS.md)

## Why Praxis Swarm?

Different models can play different roles in a project. You might want a capable model to work through the design and coordinate the team, while other models carry out well-defined assignments.

Praxis Swarm gives that collaboration a structure:

- **The lead prepares the work.** It reads the project’s requirements and develops worker assignments.
- **Workers receive clear handoffs.** Each gets its task, relevant context, responsibilities, and expected results.
- **Independent work runs in parallel.** Workers use separate workspaces based on the same source commit.
- **The lead reviews the outcome.** It inspects the combined result, runs checks, and reports what passed or needs attention.

Praxis Swarm builds on [Agent Orchestrator](https://github.com/OrchestratorInc/agent-orchestrator), adding a structured planning, handoff, execution, and review workflow to its agent-management foundation.

## From your goal to coordinated work

Ask your AI lead to use Praxis Swarm for a suitable task and specify the worker model and reasoning effort.

For example:

> “Use Praxis Swarm to build the duration parser and formatter described in this project. Delegate the implementation to Luna with low reasoning, then review and test the combined result.”

### The lead prepares the plan

The lead reads the project’s requirements and relevant files, then decides how to divide the work.

It creates a proposal describing each worker’s assignment and how the pieces should fit together. Praxis Swarm’s plan builder adds source information, context fingerprints, and recorded tool identities, then validates the proposal.

If the proposal is incomplete or contains conflicting assignments, the lead receives feedback and can correct it before workers start.

### Each worker receives an assignment packet

Praxis Swarm turns each assignment into an **assignment packet**: a structured brief that gives the worker a clear starting point.

A packet brings together:

- **The job:** What the worker is being asked to accomplish.
- **Its responsibilities:** The files or areas assigned to it, along with work outside its scope.
- **How the pieces fit:** Interfaces, expected behavior, and relevant error rules.
- **The context:** Supplied specifications and reference material selected for the task.
- **The expected result:** Acceptance criteria, recorded tools, and instructions for reporting results or blockers.

For a parser-and-formatter project, each worker receives its own assignment while sharing the specification needed to make the components work together.

The packet stays connected to the run record, so the lead can inspect **what a worker was given alongside what it produced**.

### Workers run in separate workspaces

Praxis Swarm starts the workers in separate worktrees created from the plan’s source commit. This gives them a shared baseline while keeping their changes separately reviewable.

The current workflow runs two workers in parallel, with a defined file scope for each.

### The lead follows the run

The lead uses Praxis Swarm’s native AO tool to prepare, start, inspect, and stop runs through the owned local daemon.

Praxis Swarm records launch intent, tracks execution through a deadline, and retains worker status and results. Uncertain outcomes remain visible so the lead can investigate rather than automatically launching replacement workers.

### The lead reviews the results

After a completed run, Praxis Swarm assembles the captured worker changes into a fresh combined review directory.

The lead can then:

- Inspect the implementations against their assignments.
- Check how the components fit together.
- Execute the plan’s acceptance checks.
- Run additional appropriate tests.
- Report defects, blockers, and observed results.

The run retains candidate files, hashes, patches, and available execution observations. Review reports and check logs provide a record of what the lead actually inspected and tested.

## What works today

Praxis Swarm is a **supervised source alpha** with a demonstrated lead-to-workers-to-review workflow.

| Capability | Available today |
|---|---|
| **Project discovery** | The managed lead asks clarifying questions and persists a versioned brief before planning |
| **Lead planning** | The lead creates a structured proposal from project requirements |
| **Plan preparation** | A builder derives source and context identities and validates the assignments |
| **Model roles** | A separately selected lead model coordinates two workers using one selected worker model and effort |
| **Worker handoffs** | Structured assignment packets with relevant supplied context |
| **Parallel execution** | Two Codex workers in separate worktrees with defined file scopes |
| **Run supervision** | Recorded launches, deadlines, status inspection, and explicit stop |
| **Lead review** | Combined candidate preparation, code inspection, check execution, and reporting |
| **Retained results** | Captured files, patches, hashes, and available execution observations |

You select the models and authorize the work. The lead coordinates the demonstrated workflow and reports the outcome; integration remains a separate decision.

## Demonstrated with Sol and Luna

In a live trial on an Apple Silicon Mac:

1. **Sol/high created the plan** from a duration specification and frozen tests.
2. **The proposal passed validation on its first submission.**
3. **Two Luna/low workers implemented the parser and formatter**, with overlapping provider turns.
4. **Sol reviewed the combined result**, executed all eight plan checks, and ran full test discovery.
5. **Independent verification passed 13 tests on both Python 3.9 and 3.14.**

Native telemetry confirmed the selected models and reasoning levels. The source project was preserved, and the scratch daemon shut down cleanly.

A separate trial also demonstrated Luna/low acting as the lead. These are successful demonstrations on a small coding fixture; broader project testing is the next step.

[Inspect and test the captured demonstration](docs/swarm/evidence/sol-luna-demo/README.md).

## Get started

- [Clarify your project with the AI lead](docs/swarm/PROJECT_DISCOVERY.md)
- [Start your first project with Codex](docs/swarm/GET_STARTED_CODEX.md)
- [Start your first project in the terminal](docs/swarm/GET_STARTED_TERMINAL.md)
- [AI lead workflow reference](docs/swarm/LEAD_REFERENCE.md)
- [Run the supervised CLI workflow](docs/swarm/SUPERVISED_WORKFLOW.md)
- [Explore the offline coordination demo](docs/swarm/QUICKSTART.md)
- [Read the demonstration evidence](docs/swarm/evidence/lead-sol-luna-confirmation.json)
- [Follow project progress](docs/swarm/PROGRESS.md)
- [Contribute](CONTRIBUTING.md)
