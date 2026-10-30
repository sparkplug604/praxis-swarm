# Candidate publication boundary

[PROGRESS.md](PROGRESS.md) owns completion and the next task. This describes the
candidate publication boundary; it is not a claim that a public release exists.

## Candidate contents

Publish the AO-derived source history plus reviewed Praxis modifications, current
documentation, the offline demo scripts and a sanitized in-repository evidence
summary. Keep inherited licenses and attribution. No private cloud checkout is
needed. A future destination remote must be separate from `upstream`.

Exclude local runtime/cache trees, `node_modules`, generated binaries, scratch
databases, model credentials, private-submodule content, raw session logs and the
research/download archives outside this checkout. Raw diagnostics may contain
local paths; public evidence must be derived and labelled, never silently edited
in place. Historical records in PROGRESS refer to local archives, not public
download links.

## Release checks

1. Resolve the intended GitHub owner/name/visibility. Inspect any existing target
   before creating or updating it. No force push or upstream publication.
2. Review all changed and new files and the retained history. A bounded current
   source pattern scan is not a full secrets audit. The inherited first-push
   gitleaks fallback scans HEAD only; perform an explicit redacted history scan
   before uploading retained history. Partial-clone missing objects must not be
   silently skipped or presented as scanned.
3. Preserve the [third-party declarations](../../THIRD_PARTY_NOTICES.md), determine
   applicable terms for the selected publication, and include modification
   notices for changed inherited files. Generated files must receive notices
   through their generation source/configuration rather than manual edits.
4. Run the documented offline demo and appropriate source/build checks. Keep
   pending/live checks separate from passes; preserve inherited baseline issues.
5. Review staged files, commit the intended candidate, push only to the selected
   destination and verify the resulting source, README links and CI outcome.

## Inherited automation

The documentation deployment, PR leaderboard comments and vendor-feed polling
jobs are restricted to the upstream repository. Existing upstream owner guards
remain intact. The retained ordinary test workflows are not proof of a fully
qualified Praxis release; their first remote runs still need inspection.

No installer/binary release, deployment, npm publication or live-provider CI is
part of this developer preview. Deliberate future release workflows should be
configured for the actual project destination.

## Public claims

Use the root [README](../../README.md), [offline quick start](QUICKSTART.md), and
[supervised CLI guide](SUPERVISED_WORKFLOW.md). GHL-03 now confirms a supplied-plan
CLI running two Luna/low workers concurrently on one Mac, with frozen changes,
usage records and observed stops. Independent checks passed for the toy fixture.
This supports the narrow supervised-alpha user story after release checks; it
does not establish automated acceptance/integration, complete sandboxing,
unattended recovery or measured token savings. The upstream desktop is attributed
separately. No public repository/release has been created by this confirmation.
