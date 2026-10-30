#!/usr/bin/env python3
"""Summarize the fixed offline demo; missing or skipped evidence cannot pass."""

import argparse
import json
import re
from datetime import datetime, timezone
from pathlib import Path


PACKAGE = "github.com/aoagents/agent-orchestrator/backend/internal/service/swarm/scheduler"
REQUIRED_TESTS = {
    "TestExactRuntimeComposesQualifiesAndPersistsParallelLaunches":
        "Parallel fake runtime launches; real planning, qualification and SQLite code; no model processes.",
    "TestSchedulerOverlappingProcessesIndependentCopiesAndDependency":
        "Two controlled local helper subprocesses overlap in independent copies; dependency gating and source preservation.",
    "TestSchedulerCancelSQLiteRetainsUnknownThenReconcilesExactAbsence":
        "Fake stop/probe with SQLite: uncertainty retains the running attempt; exact absence later permits cancellation.",
    "TestExactCancelAcknowledgementRequiresIndependentObservation":
        "Fake stop/probe matrix: PTY acknowledgement, unknown liveness, mismatched replies and errors.",
}


def summarize(lines, go_exit_code):
    counts = {name: {"run": 0, "pass": 0, "skip": 0, "fail": 0} for name in REQUIRED_TESTS}
    failures = []
    package_passes = 0
    elapsed = None
    for number, line in enumerate(lines, 1):
        if not line.strip():
            continue
        try:
            event = json.loads(line)
        except (ValueError, TypeError):
            failures.append(f"Invalid JSON event on line {number}.")
            continue
        if not isinstance(event, dict):
            failures.append(f"Invalid event object on line {number}.")
            continue
        action, name = event.get("Action"), event.get("Test")
        if not isinstance(action, str) or not isinstance(event.get("Package"), str) or (name is not None and not isinstance(name, str)):
            failures.append(f"Invalid event fields on line {number}.")
            continue
        if action in ("skip", "fail"):
            failures.append(f"{action}: {name or event.get('Package', 'unknown package')}")
        if event.get("Package") != PACKAGE:
            continue
        if isinstance(name, str) and name in counts and action in counts[name]:
            counts[name][action] += 1
        if action == "pass" and not name:
            package_passes += 1
            elapsed = event.get("Elapsed")
    if go_exit_code != 0:
        failures.append(f"go test exited {go_exit_code}.")
    if package_passes != 1:
        failures.append(f"Expected one package pass; observed {package_passes}.")
    tests = []
    for name, scope in REQUIRED_TESTS.items():
        count = counts[name]
        passed = count == {"run": 1, "pass": 1, "skip": 0, "fail": 0}
        if not passed:
            failures.append(f"Required test did not run and pass exactly once: {name} ({count}).")
        tests.append({"name": name, "status": "passed" if passed else "failed", "events": count, "scope": scope})
    return {
        "schema_version": 1,
        "generated_at": datetime.now(timezone.utc).isoformat(),
        "status": "passed" if not failures else "failed",
        "evidence_mode": "test",
        "go_exit_code": go_exit_code,
        "package": PACKAGE,
        "package_elapsed_seconds": elapsed,
        "required_tests": tests,
        "failures": failures,
        "limits": [
            "No model sessions or provider calls are exercised; model overlap is not demonstrated.",
            "Helper subprocess overlap is separate from fake runtime/configuration checks.",
            "This demo does not qualify live dispatch, OS sandboxing or descendant containment.",
            "Go may download its toolchain or module dependencies before testing if they are not cached.",
        ],
        "logs": {"events": "go-test.jsonl", "stderr": "go-test.stderr.log", "command": "command.txt"},
    }


def markdown(report):
    lines = ["# Offline swarm demo", "", f"Result: **{report['status'].upper()}** (Go exit {report['go_exit_code']}).",
             "", "Evidence mode: **test**. No model sessions or provider calls are exercised.",
             "", "| Required test | Result | Evidence scope |", "| --- | --- | --- |"]
    for test in report["required_tests"]:
        lines.append(f"| `{test['name']}` | {test['status']} | {test['scope']} |")
    if report["failures"]:
        lines.extend(["", "Failures:", ""])
        lines.extend(f"- {reason}" for reason in report["failures"])
    lines.extend(["", "Limits:", ""])
    lines.extend(f"- {limit}" for limit in report["limits"])
    lines.extend(["", "Raw evidence: [JSON events](go-test.jsonl), [stderr](go-test.stderr.log), [command](command.txt).", ""])
    return "\n".join(lines)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--pattern", action="store_true", help="Print the exact anchored Go test selection.")
    parser.add_argument("output_dir", nargs="?", type=Path)
    parser.add_argument("go_exit_code", nargs="?", type=int)
    args = parser.parse_args()
    if args.pattern:
        print("^(" + "|".join(re.escape(name) for name in REQUIRED_TESTS) + ")$")
        return 0
    if args.output_dir is None or args.go_exit_code is None:
        parser.error("output_dir and go_exit_code are required unless --pattern is used")
    try:
        with (args.output_dir / "go-test.jsonl").open(encoding="utf-8") as events:
            report = summarize(events, args.go_exit_code)
    except (OSError, UnicodeError) as error:
        report = summarize([], args.go_exit_code)
        report["failures"].insert(0, f"Cannot read raw test events: {error}")
    # The runner created an exclusive directory; refuse to overwrite old reports.
    with (args.output_dir / "report.json").open("x", encoding="utf-8") as output:
        json.dump(report, output, indent=2)
        output.write("\n")
    with (args.output_dir / "report.md").open("x", encoding="utf-8") as output:
        output.write(markdown(report))
    print(f"Offline demo {report['status']}: {len(REQUIRED_TESTS)} required tests; evidence mode=test.")
    return 0 if report["status"] == "passed" else 1


if __name__ == "__main__":
    raise SystemExit(main())
