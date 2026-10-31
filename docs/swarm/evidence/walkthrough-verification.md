# First-project walkthrough verification

**30 October 2026 — both setup → planning → workers → review → cleanup paths completed.**
Tested on macOS arm64 with Go 1.27.1 and Codex CLI 0.166.0-alpha.5.1, using an
existing account login. Each path cloned the public repository and built its own
CLI from implementation commit `a405d7abcfd2e9b66ed09fd1a14489cef093f164`.
The candidate guides were read locally before publication.

| Observed result | Terminal walkthrough | Codex-assisted walkthrough |
| --- | --- | --- |
| Fresh managed team | Sol/high lead + two Luna/low workers | Sol/high lead + two Luna/low workers |
| Lead-authored proposal | Prepared and validated | Prepared and validated |
| Worker execution | Two completed turns, 18.416s observed overlap | Two completed turns, 8.982s observed overlap |
| Actual lead checks | Eight declared commands passed; 10-test discovery passed | Six declared commands passed; 10-test discovery passed |
| Independent check | 13 existing tests passed on Python 3.9.6 and 3.14.3 | 13 existing tests passed on Python 3.9.6 and 3.14.3 |
| Review decision | **Changes required:** additional parser defect found | Passed for the inspected contract and observed checks |
| Preservation and cleanup | Source unchanged, clone clean, owned daemon stopped | Source unchanged, clone clean, owned daemon stopped |

## A completed review can hold a candidate for repair

The terminal lead found that `parse_duration("123456:00:0")` and
`parse_duration("12345:00:00")` raised `IndexError` instead of the required
`ValueError("invalid duration")`. Its supplemental check failed, and its report
explicitly declined acceptance. Independent reproduction confirmed both cases
on Python 3.9.6 and 3.14.3. The Codex-assisted candidate rejected both correctly.

The 10 supplied tests and the three previously retained independent tests did
not cover those truncated-input cases. Passing them is evidence about those
checks, not exhaustive correctness. Frozen submissions and original logs were
preserved; neither the setup assistant nor the lead repaired the helper files.
No replacement run, model substitution or automatic integration occurred.

This is why the walkthroughs direct users to read the review decision and findings,
even when all supplied tests pass. The terminal candidate needs a separately
scoped correction before acceptance.

## What was checked

- Literal terminal code blocks ran in Bash, including prerequisite checks, fresh
  clone/build, fixture generation, role clarification, daemon readiness, explicit
  lead creation, CLI monitoring and shutdown. The final published executable
  blocks match the tested ones; explanatory prose was improved afterward.
- For the natural-language path, the existing Codex desktop chat consumed the
  candidate guide's request and performed the same setup in a second fresh
  project. Its managed lead and two workers were fresh model sessions. This
  was a Codex-assisted execution test, not a blind usability study in a new chat.
- Native provider transcripts confirmed model, reasoning effort, workspace-write
  and disabled shell networking for all six distinct provider sessions. The lead
  used native AO swarm tools for prepare/start/get/review. Shell loopback remains
  restricted; it is not needed for those managed-lead operations.
- Every declared lead check was matched to its argv, working directory, observed
  exit status, retained log and successful native shell execution. Candidate
  hashes and changed-file scopes were independently checked before testing.
- Each combined candidate was reconstructed from its frozen baseline and captured
  worker files. The unchanged independent evaluator was reused; no oracle was
  rewritten to make a result pass. Its SHA-256 is recorded in the JSON summary.
- Each daemon exited successfully, its health endpoint stopped responding and
  its run file disappeared. Existing implementation and game work were preserved.

## First-run fixes included in the guides

The guides separate Codex setup from the managed lead role, clarify worker-only
fixture instructions, move prewritten generator plans out of the lead's inputs,
wait for verified daemon readiness, set model/effort/approval explicitly, explain
native tool access, identify where reviewed code lives, and show scoped cleanup.
The old lead-workflow URL now helps readers choose a starting path; its technical
content is retained in the reference guide.

[Machine-readable verification summary](walkthrough-verification.json) retains
sanitized run identities, candidate hashes, check results and native usage counts.
Run IDs belong to a daemon's data store; identical request IDs in two isolated
stores can produce the same run ID. Raw transcripts, account material and local
paths remain private. Native counters do not establish billed cost or savings.
These two small Mac trials do not establish general task or platform coverage.

[Codex walkthrough](../GET_STARTED_CODEX.md) ·
[Terminal walkthrough](../GET_STARTED_TERMINAL.md) ·
[Technical reference](../LEAD_REFERENCE.md)
