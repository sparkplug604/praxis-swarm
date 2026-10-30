# Third-party declarations

Praxis Swarm preserves the public Agent Orchestrator source history and inherited
license declarations. This inventory does not replace component license texts
or silently relicense upstream files.

| Material | Pinned source / declaration | Treatment |
| --- | --- | --- |
| Agent Orchestrator foundation | `OrchestratorInc/agent-orchestrator` at `71bb19f5947a64853ee6d11225bc896563878c50`; root [Apache-2.0 LICENSE](LICENSE) | Preserve original history, attribution and file notices. No root upstream NOTICE was found in the inspected snapshot. |
| AO frontend, product-ui and published CLI/platform package manifests | Declare MIT, while the root declares Apache-2.0 | Retain each declaration and supply [MIT text](licenses/MIT.txt) alongside the root license. See the [source-release treatment](docs/swarm/LICENSING.md); no blanket relicense is applied. |
| Native wake helper | [Inherited third-party notices](frontend/native/wake-helper/THIRD_PARTY_NOTICES.md) | Retain with the source. |
| Agent artwork | [MiMo Code notice](frontend/src/renderer/assets/agents/LICENSE-mimo-code.txt), [CodeWhale notice](frontend/src/renderer/assets/agents/LICENSE-codewhale.txt) | Retain asset-specific terms; inherited branding does not imply endorsement. |
| OpenSwarm selection/routing concepts | `surlymochan/openswarm` at `b2a70d436ad72f0c5b036730fc86b749ddc68af7`; Apache-2.0 | Referenced concepts were implemented in new Go code; the upstream Python launcher was not copied into this fork. |
| OpenHands usage-metric concepts | `OpenHands/software-agent-sdk` at `47bbc5e8392c4c75b7f77a1da38bed4a9f53d6a5`; MIT, copyright 2026 OpenHands contributors | Referenced concepts were implemented in new Go code; no SDK implementation is bundled by this preview. |

Dependency lockfiles retain the declared dependency versions. This is a source
preview, not a repackaging of dependency binaries or an installer. Before binary
distribution, inventory bundled dependencies and their notices separately.

See [source-release license treatment](docs/swarm/LICENSING.md), [NOTICE](NOTICE),
and [provenance](docs/swarm/UPSTREAM.md) for source links and declared boundaries,
and [publication checks](docs/swarm/PUBLICATION.md) for outstanding release work.
