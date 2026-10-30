# Praxis Swarm development

Start with the [public preview README](README.md) and
[offline quick start](docs/swarm/QUICKSTART.md).

This is the development fork for a reusable multi-model engineering swarm. The
upstream Agent Orchestrator desktop still carries its original branding. The supplied-plan supervised CLI has a confirmed two-worker demo. The separate
exact-copy scheduler and its stronger live qualification remain in development. Local qualification
contracts, durable swarm state, planning/handoff composition, and local routing
and usage admission are implemented;
a working upstream desktop does not prove live swarm capabilities.

Local scheduling, capacity and filesystem-isolation foundations are also
implemented. An internal exact-workspace bridge and durable execution receipts
are tested locally; live runtime qualification and daemon wiring remain open. See
[scheduling and workspace contracts](docs/swarm/SCHEDULING_AND_WORKSPACES.md).

Start with [current progress](docs/swarm/PROGRESS.md), then the
[full build plan](docs/swarm/BUILD_PLAN.md). Use the
[Mac setup instructions](docs/swarm/MAC_BASELINE.md) to reproduce the baseline.
[Upstream provenance](docs/swarm/UPSTREAM.md) records source and license boundaries.
[Planning contracts](docs/swarm/PLANNING_AND_HANDOFF.md) explain worker packets,
context selection, resume checks and their current limits.
[Routing and usage](docs/swarm/ROUTING_AND_USAGE.md) explains model selection,
atomic reservations, receipts, replay and remaining provider-integration limits.

The existing root README and AGENTS.md document the inherited architecture. Do
not use the earlier game-specific handoff or experimental boards as this fork's
work queue. `docs/swarm/PROGRESS.md` is the single build-progress authority.
