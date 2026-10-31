# Bounded duration contract

Implement two independent pure Python helpers using only the standard library.
The supported duration is an integer from 0 through 1_000_000_000_000 milliseconds,
inclusive. This bound is intentional; arbitrary-magnitude values are unsupported.

Canonical text is ASCII H:MM:SS.mmm. Hours are either 0 or one to six digits with
no leading zero; minutes and seconds have two digits each in 00..59; milliseconds
have exactly three digits. No signs, spaces, newlines, Unicode digits or extra
characters are accepted. The numeric total must remain within the bound.
Examples: 0 -> 0:00:00.000; 3_661_001 -> 1:01:01.001;
1_000_000_000_000 -> 277777:46:40.000.

## Parser

duration_parser.parse_duration(text) returns the exact integer milliseconds.
Only str instances are accepted. Every other type raises
TypeError("duration must be a string"). A str that violates syntax or range raises
ValueError("invalid duration"). Extremely long strings must produce this same
ValueError without relying on Python's integer-string conversion limit.

## Formatter

duration_formatter.format_duration(milliseconds) returns canonical text.
Require type(milliseconds) is int: booleans, int subclasses and all other types
raise TypeError("milliseconds must be an integer"). Integers below zero or above
the bound raise ValueError("milliseconds out of range"). Type validation comes
before range validation. Huge integers must reject without converting to text.

Both helpers have no side effects and do not mutate inputs or global state.
The parser task may edit only duration_parser.py; the formatter task may edit
only duration_formatter.py. Tests, this contract, AGENTS.md and PROGRESS.md are
frozen. Run the named tests, report evidence and stop; do not change another task.
