# Start with Project Discovery

Tell your managed Praxis Swarm lead what you want to accomplish, even when the
idea is incomplete. The lead reads relevant project files, asks material questions
in small batches, and records your answers in a persistent project brief. It uses
that brief to prepare the worker plan when the required decisions are supplied.

## Ask the lead

In an existing AO-managed project chat:

> Use Project Discovery. I want a dashboard for our operations team, but I need
> help defining the first useful version. Inspect the project, ask the important
> unanswered questions, and save our decisions before preparing worker assignments.

The lead should ask about missing decisions instead of repeating questions already
answered in project files. Its brief records the goal, audience, outcomes,
acceptance criteria, constraints, explicit assumptions and questions with answers.
A draft can remain incomplete across turns and daemon restarts.

Reply in the same managed chat. From a terminal connected to that daemon, you can
also send the reply with a stable delivery ID:

```bash
ao send --session LEAD_ID --steer --client-message-id discovery-answer-1 \
  --message 'The users are operations staff; start with filtering and inspecting records.'
```

Use a new delivery ID for a different reply. The lead reads the stored brief,
records the new information and asks further questions only when needed.
For initial local setup, use the [Codex](GET_STARTED_CODEX.md) or
[terminal](GET_STARTED_TERMINAL.md) walkthrough. Ordinary Codex chats do not
acquire the managed lead's tools automatically.

## Inspect the saved brief

```bash
ao swarm discovery get PROJECT_ID
ao swarm discovery get PROJECT_ID --revision 1
```

The daemon derives `needs_input` or `ready`. A ready brief has a goal, audience,
at least one outcome, at least one acceptance criterion, and answers to all
required questions. Unanswered optional questions do not block readiness.
Readiness describes the recorded data; it is not proof of human approval or code
correctness. Assumptions remain explicitly identified.

The lead uses native `swarm` actions `discovery_get` and `discovery_save`, so its
shell does not need loopback access. Discovery data lives in the daemon's existing
SQLite database, outside project source. It does not dirty the source checkout.

## Revise without losing earlier decisions

Every save appends an immutable revision. It supplies `expectedRevision` to avoid
overwriting a concurrent edit; zero creates the first revision. A stale update
returns a conflict and the caller must read the latest version.

For a direct terminal update, put the complete draft in a JSON file:

```json
{
  "expectedRevision": 0,
  "brief": {
    "goal": "Build an operations dashboard",
    "audience": "Operations staff",
    "outcomes": ["Find and inspect records"],
    "acceptance": ["Filtering changes the visible records"],
    "constraints": [],
    "assumptions": [],
    "questions": [
      {"id": "data", "question": "Which data source?", "required": true, "answer": ""}
    ]
  }
}
```

```bash
ao swarm discovery save PROJECT_ID --input discovery-update.json
```

An unanswered question uses an empty string, never JSON `null`. Updates supply the
whole current draft; earlier revisions remain readable. Input size, strings,
question counts and duplicate JSON keys are validated before persistence.

## Connect discovery to execution

For a project with a discovery record, the proposal must include `brief` containing
the ready record's exact `revision` and `sha256`. The lead copies these values from
the daemon response. The builder includes the supplied brief in worker context
and binds the reference into the plan and packet identities.

Preparation and new run creation reject an incomplete, missing or stale binding.
Editing the brief invalidates an older prepared plan, including a cached native
prepared handle. The lead reads the current revision and prepares again.

Already-started runs keep their recorded contract. Historical run retrieval and
same-request recovery remain available; revising a brief does not launch a
replacement or automatically cancel ongoing work. Discovery itself starts no
workers and grants no execution permission.

The existing execution profile still coordinates two independent Codex workers
with one explicitly selected worker model and effort. Projects using complete
supplied plans without discovery records retain that workflow.

API: GET/POST `/api/v1/swarm/discovery/{projectId}`; GET accepts optional
`revision`. Generated OpenAPI and TypeScript schemas describe the payloads.

[Live confirmation](evidence/project-discovery-confirmation.md) ·
[Lead workflow](LEAD_WORKFLOW.md) · [Technical reference](LEAD_REFERENCE.md)
