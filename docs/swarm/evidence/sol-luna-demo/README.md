# Sol/high lead, Luna/low workers: captured toy result

These are selected unchanged files from the independently assembled, frozen
result of the 30 October 2026 Mac trial. The two helpers are the Luna workers'
outputs. The contract and original suites preceded execution; the independent
oracle is the unchanged GHL-03 evaluator. Raw conversations, credentials and
scratch daemon state are excluded.

From this directory, run:

```bash
python3 -B -m unittest discover -v
```

Expected: 13 passing tests, including 1,010 independent oracle/roundtrip values.
Recorded runs passed on Python 3.9.6 and 3.14.3. This command runs no models.
[source-hashes.json](source-hashes.json) binds the selected files;
[trial evidence](../lead-sol-luna-confirmation.json) records observed model settings,
worker overlap, review checks and assurance limits. Rerunning the code checks the
captured implementation; it does not independently prove the historical model run.

Recorded outputs: [Python 3.9](python39-independent-tests.txt) and
[Python 3.14](python314-independent-tests.txt).
