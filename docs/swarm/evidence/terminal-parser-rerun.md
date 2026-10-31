# Terminal parser correction and rerun

**30 October 2026 — correction and fresh terminal rerun passed.**
The [original failure report](walkthrough-verification.md) and
[original JSON evidence](walkthrough-verification.json) remain byte-for-byte unchanged.

## Correcting the captured parser

The original parser indexed a separator before checking the field-dependent
length. The correction moves that length check before indexing. Truncated strings
now raise `ValueError("invalid duration")` as specified.

[Before/after code and runnable regression](terminal-parser-correction/README.md)
are retained separately from the original snapshots. The regression tests every
proper prefix of six valid strings covering one through six hour digits.

| Check | Python 3.9.6 | Python 3.14.3 |
| --- | --- | --- |
| Regression against original parser | Failed as expected | Failed as expected |
| Regression against corrected parser | Passed | Passed |
| Corrected original candidate, full combined suite | 14 tests passed | 14 tests passed |

The same regression was added to newly generated fixture tests. The generator's
six local validation tests passed. New fixtures now contain 11 tests; historical
frozen tests and prior results were preserved.

## Fresh terminal walkthrough

A new disposable project was generated from candidate commit
`b211f27736236cf3066661c960d91a0b7408fe42`. For this prepublication check, the
walkthrough cloned that candidate locally instead of its GitHub URL. The remaining
setup, session creation, monitoring and cleanup commands were reused.

- One **Sol/high** lead created and validated its own proposal.
- Two **Luna/low** workers implemented the unfinished helpers independently in
  disjoint scopes. Their observed provider turns overlapped by **13.303 seconds**.
- The lead inspected the combined candidate and actually executed all **nine
  declared checks**, including the truncated-input check. All passed.
- Lead-run unittest discovery passed **11 tests**. The review reported
  `reviewed_passed_candidate`, with no findings or remaining issues.
- Independent reconstruction and testing passed **14 tests** on both Python
  versions: the 11 fixture tests plus the unchanged three-test independent oracle.
- Native telemetry confirmed the requested models, reasoning settings,
  workspace-write and disabled shell networking for three fresh provider sessions.
- Source files were preserved, the clone stayed clean, and the owned daemon
  exited successfully with its health endpoint and run file gone.

The hand-corrected original parser and the fresh worker-authored parser are
separate candidates. The rerun did not seed workers with a finished solution or
rewrite the failed submission. No replacement run, model substitution, automatic
merge or game-code change occurred.

[Machine-readable evidence](terminal-parser-rerun.json) includes source and
candidate identities, actual declared-check results, correction hashes and
preservation checks. Raw logs remain private. These results cover this Mac fixture;
they do not establish general coding or platform coverage.

[Terminal guide](../GET_STARTED_TERMINAL.md) ·
[Original failure](walkthrough-verification.md) ·
[Reproduce the correction](terminal-parser-correction/README.md)
