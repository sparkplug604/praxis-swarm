# Terminal parser correction

This is a correction to the terminal walkthrough's captured parser, not a change
to Praxis Swarm's daemon. The [original failure report](../walkthrough-verification.md)
and its machine-readable evidence remain unchanged.

`before/duration_parser.py` is an exact copy of the original failed candidate.
`after/duration_parser.py` moves the field-dependent length check before separator
indexing. Truncated inputs therefore raise the required `ValueError("invalid duration")`
instead of `IndexError`. The formatter and original submission are untouched.

From this directory:

```bash
# Expected failure: reproduces the original defect.
PYTHONPATH=before python3 -B -m unittest -v test_truncated_duration

# Expected success: checks the corrected candidate.
PYTHONPATH=after python3 -B -m unittest -v test_truncated_duration
```

The regression checks every proper prefix of six representative valid strings,
covering one through six hour digits. Both commands were run on Python 3.9.6 and
3.14.3; see [observed regression exits](regression-results.json).
The same regression is now included in newly generated fixture parser suites.
Original frozen tests and failure records have not been rewritten.
