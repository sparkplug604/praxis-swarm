"""Offline report failures must never turn into successful demo evidence."""

import importlib.util
import json
import sys
import unittest
from pathlib import Path


sys.dont_write_bytecode = True
spec = importlib.util.spec_from_file_location("summarize_demo", Path(__file__).with_name("summarize-demo.py"))
demo = importlib.util.module_from_spec(spec)
spec.loader.exec_module(demo)


def complete_events():
    events = []
    for name in demo.REQUIRED_TESTS:
        events.extend([{"Action": "run", "Test": name, "Package": demo.PACKAGE},
                       {"Action": "pass", "Test": name, "Package": demo.PACKAGE}])
    events.append({"Action": "pass", "Package": demo.PACKAGE, "Elapsed": 1.0})
    return events


def report(events, exit_code=0):
    return demo.summarize([json.dumps(event) for event in events], exit_code)


class ReportTests(unittest.TestCase):
    def test_complete_run_passes(self):
        result = report(complete_events())
        self.assertEqual(result["status"], "passed")
        self.assertEqual(result["failures"], [])

    def test_missing_skip_nonzero_and_invalid_events_fail(self):
        required = next(iter(demo.REQUIRED_TESTS))
        cases = {
            "missing": [event for event in complete_events() if event.get("Test") != required],
            "top-level skip": complete_events() + [{"Action": "skip", "Test": required, "Package": demo.PACKAGE}],
            "subtest skip": complete_events() + [{"Action": "skip", "Test": required + "/case", "Package": demo.PACKAGE}],
            "subtest failure": complete_events() + [{"Action": "fail", "Test": required + "/case", "Package": demo.PACKAGE}],
            "package incomplete": complete_events()[:-1],
            "duplicate pass": complete_events() + [{"Action": "pass", "Test": required, "Package": demo.PACKAGE}],
            "wrong package": [dict(event, Package="other") for event in complete_events()],
        }
        for name, events in cases.items():
            with self.subTest(name=name):
                self.assertEqual(report(events)["status"], "failed")
        self.assertEqual(report(complete_events(), 7)["status"], "failed")
        self.assertEqual(demo.summarize(["invalid", "[]"], 0)["status"], "failed")


if __name__ == "__main__":
    unittest.main()
