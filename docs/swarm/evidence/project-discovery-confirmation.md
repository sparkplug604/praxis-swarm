# Project Discovery confirmation

A live Mac fixture completed the discovery-to-execution workflow using a Sol/high
lead and two Luna/low workers, with Standard explicitly configured for launched
sessions. Scripted test-user answers were used; this is an integration check,
not a general project-management quality benchmark.

1. The lead inspected the project, saved revision 1 with required unanswered
   questions, asked for clarification, and launched no workers.
2. After a daemon restart, the same brief was retrieved unchanged and the same
   lead resumed its native conversation.
3. Scripted answers produced ready revision 2. The proposal and execution plan
   referenced that exact revision and SHA-256.
4. Both workers completed in separate scopes, with 12.515 seconds of observed
   provider-turn overlap. The original source stayed unchanged.
5. The lead inspected the combined result and actually executed all eight declared
   checks plus 11-test discovery. Its review passed without findings.
6. Independently reconstructed candidates passed all 14 tests on Python 3.9.6
   and 3.14.3, using the unchanged prior independent evaluator.
7. The owned daemon exited successfully; its health endpoint and run file were gone.

The initial one-lead probe exposed a masked HTTP error and an insufficiently typed
question schema. Both were repaired and covered by regression tests; no workers
were launched in that probe. A subsequent harness reply omitted `--steer`, so it
failed before delivery. The corrected command resumed the same lead and durable
brief instead of creating another intake agent. Those attempts and their usage
remain in the local evidence record.

[Sanitized structured results](project-discovery-confirmation.json) retain the
binding, candidate identities, native settings and actual-check evidence.
Raw transcripts and account balances remain private. The desktop implementation
chat's speed could not be machine-verified; the live-session Standard override
was verified through the Codex configuration interface.

[Project Discovery guide](../PROJECT_DISCOVERY.md)
