# Source-release license treatment

The source release preserves the upstream Apache-2.0 [LICENSE](../../LICENSE),
including its `Copyright 2026 Untrivial` notice. New Praxis Swarm source follows
that root license unless a file states otherwise. Existing license and
asset-specific notices are retained; this fork does not replace them.

Seven inherited package manifests declare MIT: `frontend`, `packages/product-ui`,
`packages/ao`, and `packages/ao-{darwin-arm64,darwin-x64,linux-x64,win32-x64}`.
The exact platform paths are recorded in the manifests; these declarations are
preserved. [MIT text](../../licenses/MIT.txt) is included with the upstream root
copyright attribution. That addition supplies the declared license text; it is
not a claim that the manifests resolve every file's relationship to the root
Apache declaration, or that all components can be relicensed under one choice.
Both sets of notices travel with this source package.

Changed inherited files carry modification notices. Generated notices come
from the API specification builder and `scripts/swarm/mark-generated.mjs`,
invoked by the documented `npm run api` and `npm run sqlc` commands. Original
upstream sources remain available in the retained Git history.

This publication distributes source, with existing asset notices and dependency
lockfiles. It does not bundle downloaded dependencies, native toolchains,
application installers or private-submodule content. A binary release needs its
own bundled-component inventory and notice review.

References: [Apache-2.0 redistribution terms](https://www.apache.org/licenses/LICENSE-2.0),
[MIT text](https://opensource.org/license/mit), and
[component inventory](../../THIRD_PARTY_NOTICES.md).
