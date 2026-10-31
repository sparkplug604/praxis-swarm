#!/usr/bin/env python3
"""Create a fresh local fixture and supplied plan for `ao swarm run`; launch nothing."""

import argparse
import hashlib
import json
import os
import re
import shlex
import shutil
import subprocess
import sys
from pathlib import Path


CONTRACT = """# Bounded duration contract

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
"""

PARSER_TESTS = '''import unittest
from duration_parser import parse_duration


class DurationParserTests(unittest.TestCase):
    def test_exact_values(self):
        cases = {"0:00:00.000": 0, "0:00:00.001": 1,
                 "0:00:59.999": 59999, "0:01:00.000": 60000,
                 "1:01:01.001": 3661001, "24:00:00.000": 86400000,
                 "277777:46:40.000": 1000000000000}
        for text, expected in cases.items():
            with self.subTest(text=text):
                actual = parse_duration(text)
                self.assertIs(type(actual), int)
                self.assertEqual(actual, expected)

    def test_independent_units(self):
        for hours in (0, 1, 13, 999, 277776):
            for minutes, seconds, millis in ((0, 0, 0), (59, 59, 999), (7, 31, 4)):
                text = f"{hours}:{minutes:02}:{seconds:02}.{millis:03}"
                expected = hours * 3600000 + minutes * 60000 + seconds * 1000 + millis
                self.assertEqual(parse_duration(text), expected)

    def test_rejects_non_strings(self):
        values = (None, True, 0, 1.5, b"0:00:00.000", [], {})
        for value in values:
            with self.subTest(value=type(value).__name__):
                with self.assertRaises(TypeError) as caught:
                    parse_duration(value)
                self.assertEqual(str(caught.exception), "duration must be a string")
        self.assertEqual(values[-2:], ([], {}))

    def test_rejects_invalid_text(self):
        values = ("", "00:00:00.000", "01:00:00.000", "-1:00:00.000",
                  "+1:00:00.000", "1:0:00.000", "1:00:0.000", "1:60:00.000",
                  "1:00:60.000", "1:00:00.00", "1:00:00.0000", " 0:00:00.000",
                  "0:00:00.000 ", "0:00:00.000\\n", "１:00:00.000", "0:٠٠:00.000",
                  "277777:46:40.001", "999999:59:59.999", "1000000:00:00.000")
        for value in values:
            with self.subTest(value=value):
                with self.assertRaises(ValueError) as caught:
                    parse_duration(value)
                self.assertEqual(str(caught.exception), "invalid duration")

    def test_rejects_truncated_fields(self):
        for complete in ("0:00:00.000", "12:34:56.789", "123:00:00.000",
                         "1234:00:00.000", "12345:00:00.000", "123456:00:00.000"):
            for end in range(len(complete)):
                text = complete[:end]
                with self.subTest(text=text):
                    with self.assertRaises(ValueError) as caught:
                        parse_duration(text)
                    self.assertEqual(str(caught.exception), "invalid duration")

    def test_huge_text_rejects_portably(self):
        with self.assertRaises(ValueError) as caught:
            parse_duration("9" * 5001 + ":00:00.000")
        self.assertEqual(str(caught.exception), "invalid duration")


if __name__ == "__main__":
    unittest.main()
'''

FORMATTER_TESTS = '''import unittest
from duration_formatter import format_duration


class DurationFormatterTests(unittest.TestCase):
    def test_exact_values(self):
        cases = {0: "0:00:00.000", 1: "0:00:00.001", 999: "0:00:00.999",
                 1000: "0:00:01.000", 59999: "0:00:59.999", 60000: "0:01:00.000",
                 3661001: "1:01:01.001", 86400000: "24:00:00.000",
                 1000000000000: "277777:46:40.000"}
        for value, expected in cases.items():
            with self.subTest(value=value):
                actual = format_duration(value)
                self.assertIs(type(actual), str)
                self.assertEqual(actual, expected)

    def test_unit_boundaries(self):
        self.assertEqual(format_duration(3599999), "0:59:59.999")
        self.assertEqual(format_duration(3600000), "1:00:00.000")
        self.assertEqual(format_duration(3600001), "1:00:00.001")
        self.assertEqual(format_duration(999999999999), "277777:46:39.999")

    def test_rejects_non_integers(self):
        class DerivedInt(int):
            pass
        values = (None, True, False, 1.0, "1", [], {}, DerivedInt(1), float("nan"))
        for value in values:
            with self.subTest(value=type(value).__name__):
                with self.assertRaises(TypeError) as caught:
                    format_duration(value)
                self.assertEqual(str(caught.exception), "milliseconds must be an integer")
        self.assertEqual(values[5:7], ([], {}))

    def test_rejects_out_of_range(self):
        for value in (-1, -1000, 1000000000001):
            with self.subTest(value=value):
                with self.assertRaises(ValueError) as caught:
                    format_duration(value)
                self.assertEqual(str(caught.exception), "milliseconds out of range")

    def test_huge_integer_rejects_portably(self):
        for value in (10 ** 5001, -(10 ** 5001)):
            with self.assertRaises(ValueError) as caught:
                format_duration(value)
            self.assertEqual(str(caught.exception), "milliseconds out of range")


if __name__ == "__main__":
    unittest.main()
'''


def fixture_files():
    return {
        ".gitignore": "__pycache__/\n*.py[cod]\n",
        "AGENTS.md": """# Fixture instructions

Read CONTRACT.md and PROGRESS.md. Implement only the single file assigned in
your packet. Do not edit tests, instructions, progress, or the other helper.
Use only Python's standard library. Run the recorded canonical Python with -B
and the named unittest checks from the repository root. Do not install packages,
access the network, commit, merge, push, or choose another task. Report changed
files, test results and remaining limits in your response, then stop.
""",
        "PROGRESS.md": """# Fixture progress

Prepared baseline: parser and formatter are intentionally unimplemented.
Each task owns one implementation file. Read this frozen record; report results
in the task response rather than editing it. No tests or model runs have passed
at generation time. Plan token values are advisory, not provider-enforced limits.
""",
        "CONTRACT.md": CONTRACT,
        "duration_parser.py": '"""Bounded canonical duration parser; see CONTRACT.md."""\n\n\ndef parse_duration(text):\n    raise NotImplementedError("parser assignment is not implemented")\n',
        "duration_formatter.py": '"""Bounded canonical duration formatter; see CONTRACT.md."""\n\n\ndef format_duration(milliseconds):\n    raise NotImplementedError("formatter assignment is not implemented")\n',
        "tests/__init__.py": "",
        "tests/test_parse_duration.py": PARSER_TESTS,
        "tests/test_format_duration.py": FORMATTER_TESTS,
    }


def digest(data):
    return hashlib.sha256(data).hexdigest()


def json_text(value):
    return json.dumps(value, ensure_ascii=False, indent=2) + "\n"


def valid_id(value):
    return (bool(value) and len(value.encode("utf-8")) <= 256 and value.strip() == value
            and not any(char in value for char in "\x00\n\r\t"))


def python_identity(requested):
    found = shutil.which(str(requested))
    if found is None:
        raise ValueError("--python must identify an existing executable")
    path = Path(found).resolve(strict=True)
    if not path.is_file() or not os.access(path, os.X_OK):
        raise ValueError("--python must resolve to an executable regular file")
    result = subprocess.run([str(path), "-I", "--version"], check=True,
                            capture_output=True, text=True, timeout=15)
    version = (result.stdout or result.stderr).strip()
    if not re.fullmatch(r"Python 3\.[0-9]+\.[0-9]+[A-Za-z0-9.+-]*", version):
        raise ValueError("--python must report a Python 3 version")
    with path.open("rb") as binary:
        sha = hashlib.sha256()
        for chunk in iter(lambda: binary.read(1024 * 1024), b""):
            sha.update(chunk)
    return {"name": "python", "path": str(path), "sha256": sha.hexdigest(), "version": version}


def git_environment():
    # Ambient GIT_DIR, config includes and hooks must never redirect the fixture
    # into another checkout or run user-configured commands.
    env = {key: value for key, value in os.environ.items() if not key.startswith("GIT_")}
    env.update(GIT_CONFIG_GLOBAL=os.devnull, GIT_CONFIG_SYSTEM=os.devnull,
               GIT_CONFIG_NOSYSTEM="1", GIT_TERMINAL_PROMPT="0")
    return env


def run_git(repository, log, *args):
    command = ["git", "-c", "core.hooksPath=" + os.devnull,
               "-c", "commit.gpgSign=false", "-c", "core.autocrlf=false",
               "-c", "core.fsmonitor=false", "-c", "user.name=Praxis Demo Fixture",
               "-c", "user.email=fixture@example.invalid", *args]
    result = subprocess.run(command, cwd=repository, env=git_environment(),
                            capture_output=True, text=True, timeout=30)
    log.write(shlex.join(command) + "\n" + result.stdout + result.stderr)
    log.flush()
    result.check_returncode()
    return result.stdout.strip()


def task_contract(name, tool, model, effort, context_refs, files):
    parser = name == "parser"
    module = "duration_parser.py" if parser else "duration_formatter.py"
    suite = "tests.test_parse_duration" if parser else "tests.test_format_duration"
    cls = "DurationParserTests" if parser else "DurationFormatterTests"
    errors = [
        {"id": "non-string" if parser else "non-integer",
         "when": "input is not a str instance" if parser else "type(milliseconds) is not int, including bool and int subclasses",
         "signal": "throw", "value": 'TypeError("duration must be a string")' if parser else 'TypeError("milliseconds must be an integer")',
         "destination": "unchanged", "effect": "Reject before conversion; inputs and global state remain unchanged."},
        {"id": "invalid-duration" if parser else "out-of-range",
         "when": "text violates canonical ASCII syntax or total is outside 0..10^12 milliseconds" if parser else "integer is outside the inclusive 0..10^12 millisecond range",
         "signal": "throw", "value": 'ValueError("invalid duration")' if parser else 'ValueError("milliseconds out of range")',
         "destination": "unchanged", "effect": "Reject without side effects, including huge inputs; do not depend on integer-string conversion limits."},
    ]
    methods = ["test_rejects_non_strings", "test_rejects_invalid_text"] if parser else ["test_rejects_non_integers", "test_rejects_out_of_range"]
    checks = [{"id": name + "-suite", "command": [tool["path"], "-B", "-m", "unittest", suite],
               "expectedExitCode": 0, "expectedResult": "All frozen tests pass, including exact boundary values and portable huge-input rejection."}]
    for rule, method in zip(errors, methods):
        checks.append({"id": name + "-" + rule["id"],
                       "command": [tool["path"], "-B", "-m", "unittest", suite + "." + cls + "." + method],
                       "expectedExitCode": 0, "expectedResult": rule["value"] + " is raised with the exact message for every invalid case; inputs remain unchanged.",
                       "errorRuleId": rule["id"]})
    return {
        "id": name, "role": "worker", "version": 1,
        "objective": "Implement " + module + " to satisfy the frozen bounded-duration contract and tests.",
        "oracleHash": digest(files[suite.replace(".", "/") + ".py"].encode("utf-8")),
        "agent": {"harness": "codex", "model": model, "effort": effort},
        "dependencies": [], "resources": [{"namespace": "repository", "kind": "path", "key": module}],
        "reserveTokens": 300000,
        "work": {
            "version": 1, "taskType": "implementation", "contextByteLimit": 32768,
            "nonGoals": ["Do not edit any file except " + module + ".",
                         "Do not add dependencies, use the network, commit, merge or implement the other task."],
            "interfaces": [{"id": name + "-function",
                            "contract": ("parse_duration(text: str) -> int" if parser else "format_duration(milliseconds: int) -> str") + "; exact behavior, inclusive 0..10^12 bound and canonical ASCII H:MM:SS.mmm are specified in CONTRACT.md.",
                            "ownership": "This worker exclusively owns " + module + ". All other fixture files are frozen.",
                            "errorRuleIds": [rule["id"] for rule in errors]}],
            "errors": errors, "acceptance": {"oracleId": name + "-duration-v1", "checks": checks},
            "context": context_refs, "tools": [tool], "progressPath": "PROGRESS.md",
            "onComplete": "submit_and_stop", "onBlocked": "report_and_stop",
        },
    }


def generate(output_dir, project_id="ghl03-demo", model="gpt-5.6-luna", effort="low", python=sys.executable):
    for label, value in (("project ID", project_id), ("model", model), ("effort", effort)):
        if not valid_id(value):
            raise ValueError("invalid " + label)
    tool = python_identity(python)
    requested = Path(os.path.abspath(os.path.expanduser(str(output_dir))))
    output = requested.parent.resolve(strict=True) / requested.name
    output.mkdir(mode=0o700)  # exclusive, including existing/dangling symlinks
    repository = output / "repository"
    repository.mkdir(mode=0o700)
    files = fixture_files()
    for name, body in files.items():
        destination = repository / name
        destination.parent.mkdir(parents=True, exist_ok=True)
        with destination.open("x", encoding="utf-8", newline="\n") as stream:
            stream.write(body)
    with (output / "generation.log").open("x", encoding="utf-8") as log:
        run_git(repository, log, "init", "--initial-branch=main", "--template=")
        run_git(repository, log, "add", "--", *sorted(files))
        run_git(repository, log, "commit", "-m", "Fixture baseline: bounded duration assignments")
        commit = run_git(repository, log, "rev-parse", "HEAD")
        if run_git(repository, log, "status", "--porcelain=v1", "--untracked-files=all"):
            raise RuntimeError("generated repository is not clean; preserved for inspection")
    manifest = {"version": 1, "kind": "caller-supplied-source-manifest", "sourceCommit": commit,
                "files": [{"path": name, "sha256": digest((repository / name).read_bytes())} for name in sorted(files)]}
    contexts = [{"id": "duration-spec", "body": CONTRACT},
                {"id": "fixture-source", "body": json_text(manifest)}]
    refs = [{"id": item["id"], "kind": "spec" if index == 0 else "source_manifest",
             "sha256": digest(item["body"].encode("utf-8")), "required": True,
             "selectors": {"taskTypes": ["implementation"], "backends": ["codex"], "paths": []}}
            for index, item in enumerate(contexts)]
    plan = {"protocolVersion": 1, "version": 1, "projectId": project_id, "sourceCommit": commit,
            "objective": "Implement and independently review two bounded duration helpers in supervised AO sessions.",
            "budgetBucket": "supervised-demo-advisory", "tokenLimit": 600000, "maxActive": 2,
            "tasks": [task_contract(name, tool, model, effort, refs, files) for name in ("parser", "formatter")]}
    # A lead supplies decisions; the daemon builder derives hashes and source facts.
    proposal = {"projectId": project_id, "objective": plan["objective"],
                "specification": CONTRACT, "model": model, "effort": effort,
                "tokenLimit": plan["tokenLimit"],
                "tools": [{key: tool[key] for key in ("name", "path", "version")}], "tasks": []}
    for task in plan["tasks"]:
        work = task["work"]
        acceptance = json.loads(json.dumps(work["acceptance"]))
        for check in acceptance["checks"]:
            check["command"][0] = tool["name"]
        proposal["tasks"].append({"id": task["id"], "objective": task["objective"],
                                  "paths": [resource["key"] for resource in task["resources"]],
                                  "nonGoals": work["nonGoals"], "interfaces": work["interfaces"],
                                  "errors": work["errors"], "acceptance": acceptance,
                                  "progressPath": work["progressPath"]})
    with (output / "proposal.json").open("x", encoding="utf-8", newline="\n") as stream:
        stream.write(json_text(proposal))
    for name, value in (("plan.json", plan), ("context.json", contexts), ("source-manifest.json", manifest)):
        with (output / name).open("x", encoding="utf-8", newline="\n") as stream:
            stream.write(json_text(value))
    # Keep one stable request key per generated directory, not per invocation.
    request_id = "duration-demo-" + digest(str(output).encode("utf-8"))[:20]
    return {"repository": str(repository), "proposal": str(output / "proposal.json"), "plan": str(output / "plan.json"),
            "context": str(output / "context.json"), "sourceCommit": commit,
            "projectAddCommand": shlex.join(["ao", "project", "add", "--path", str(repository), "--id", project_id]),
            "swarmRunCommand": shlex.join(["ao", "swarm", "run", "--plan", str(output / "plan.json"), "--context", str(output / "context.json"), "--request-id", request_id, "--timeout", "240"]),
            "note": "Nothing launched. Stubs intentionally fail acceptance. Token values are advisory; actual provider usage is not capped or guaranteed."}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output-dir", required=True, type=Path, help="New directory; parent must exist. Existing or failed output is never overwritten.")
    parser.add_argument("--project-id", default="ghl03-demo")
    parser.add_argument("--model", default="gpt-5.6-luna")
    parser.add_argument("--effort", default="low")
    parser.add_argument("--python", default=sys.executable, help="Python 3 interpreter to pin by canonical path, hash and reported version.")
    args = parser.parse_args()
    try:
        result = generate(args.output_dir, args.project_id, args.model, args.effort, args.python)
    except (OSError, ValueError, RuntimeError, subprocess.SubprocessError) as error:
        print("Fixture generation failed: " + str(error) + ". Any created output is retained; choose a new directory to try again.", file=sys.stderr)
        return 1
    print(json_text(result), end="")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
