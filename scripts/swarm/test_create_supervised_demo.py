"""Offline generator checks: provenance, scoped contracts and preserved output."""

import hashlib
import importlib.util
import json
import os
import shlex
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path
from unittest import mock


sys.dont_write_bytecode = True
SCRIPT = Path(__file__).with_name("create-supervised-demo.py")
spec = importlib.util.spec_from_file_location("create_supervised_demo", SCRIPT)
demo = importlib.util.module_from_spec(spec)
spec.loader.exec_module(demo)


class GeneratorTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix="supervised-generator-test-")
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)

    def git(self, repository, *args):
        return subprocess.check_output(["git", "-C", str(repository), *args],
                                       env=demo.git_environment(), text=True).strip()

    def test_cli_emits_clean_main_fixture_with_bound_provenance(self):
        output = self.root / "fixture with spaces"
        result = subprocess.run([sys.executable, "-B", str(SCRIPT), "--output-dir", str(output)],
                                check=True, capture_output=True, text=True, timeout=30)
        receipt = json.loads(result.stdout)
        repository = Path(receipt["repository"])
        plan = json.loads(Path(receipt["plan"]).read_text())
        contexts = {item["id"]: item["body"] for item in json.loads(Path(receipt["context"]).read_text())}
        manifest = json.loads((output / "source-manifest.json").read_text())
        self.assertEqual(self.git(repository, "branch", "--show-current"), "main")
        self.assertEqual(self.git(repository, "status", "--porcelain=v1", "--untracked-files=all"), "")
        self.assertEqual(plan["sourceCommit"], self.git(repository, "rev-parse", "HEAD"))
        self.assertEqual(manifest["sourceCommit"], plan["sourceCommit"])
        self.assertEqual(receipt["sourceCommit"], plan["sourceCommit"])
        self.assertEqual(json.loads(contexts["fixture-source"]), manifest)
        self.assertEqual(contexts["duration-spec"], (repository / "CONTRACT.md").read_text())
        tracked = set(self.git(repository, "ls-files").splitlines())
        self.assertEqual(tracked, {entry["path"] for entry in manifest["files"]})
        for entry in manifest["files"]:
            raw = (repository / entry["path"]).read_bytes()
            self.assertEqual(entry["sha256"], hashlib.sha256(raw).hexdigest())
            if entry["path"].endswith(".py"):
                compile(raw, entry["path"], "exec")
        self.assertEqual(plan["protocolVersion"], 1)
        self.assertEqual(plan["version"], 1)
        self.assertEqual(plan["projectId"], "ghl03-demo")
        self.assertEqual(plan["maxActive"], 2)
        self.assertEqual(plan["tokenLimit"], 600000)
        self.assertEqual({task["id"] for task in plan["tasks"]}, {"parser", "formatter"})
        self.assertEqual(sum(task["reserveTokens"] for task in plan["tasks"]), 600000)
        scopes = []
        for task in plan["tasks"]:
            self.assertEqual(task["role"], "worker")
            self.assertEqual(task["version"], 1)
            self.assertEqual(task["agent"], {"harness": "codex", "model": "gpt-5.6-luna", "effort": "low"})
            self.assertEqual(task["dependencies"], [])
            self.assertEqual(task["reserveTokens"], 300000)
            self.assertEqual(task["resources"], [{"namespace": "repository", "kind": "path",
                                                  "key": "duration_" + task["id"] + ".py"}])
            scopes.append(task["resources"][0]["key"])
            work = task["work"]
            self.assertEqual(work["version"], 1)
            self.assertEqual(work["contextByteLimit"], 32768)
            self.assertEqual(work["progressPath"], "PROGRESS.md")
            self.assertEqual(work["onComplete"], "submit_and_stop")
            self.assertEqual(work["onBlocked"], "report_and_stop")
            self.assertEqual([ref["kind"] for ref in work["context"]], ["spec", "source_manifest"])
            for ref in work["context"]:
                self.assertTrue(ref["required"])
                self.assertEqual(ref["sha256"], hashlib.sha256(contexts[ref["id"]].encode()).hexdigest())
                self.assertEqual(ref["selectors"], {"taskTypes": ["implementation"], "backends": ["codex"], "paths": []})
            tool, = work["tools"]
            self.assertEqual(Path(tool["path"]), Path(sys.executable).resolve())
            self.assertEqual(tool["sha256"], hashlib.sha256(Path(tool["path"]).read_bytes()).hexdigest())
            self.assertRegex(tool["version"], r"^Python 3\.")
            errors = {rule["id"] for rule in work["errors"]}
            self.assertEqual(errors, set(work["interfaces"][0]["errorRuleIds"]))
            checks = work["acceptance"]["checks"]
            self.assertEqual(errors, {check["errorRuleId"] for check in checks if "errorRuleId" in check})
            for check in checks:
                self.assertEqual(check["command"][:4], [tool["path"], "-B", "-m", "unittest"])
                self.assertEqual(check["expectedExitCode"], 0)
            suite = checks[0]["command"][-1]
            self.assertEqual(task["oracleHash"], hashlib.sha256((repository / (suite.replace(".", "/") + ".py")).read_bytes()).hexdigest())
            # These are runnable assignments, and baseline acceptance must fail.
            baseline = subprocess.run(checks[0]["command"], cwd=repository, capture_output=True, text=True, timeout=15)
            self.assertEqual(baseline.returncode, 1)
            self.assertIn("NotImplementedError", baseline.stderr)
            self.assertIn("Ran 6 tests" if task["id"] == "parser" else "Ran 5 tests", baseline.stderr)
        self.assertEqual(len(set(scopes)), 2)
        self.assertEqual(self.git(repository, "status", "--porcelain=v1", "--untracked-files=all"), "")
        self.assertEqual(shlex.split(receipt["projectAddCommand"]),
                         ["ao", "project", "add", "--path", str(repository), "--id", "ghl03-demo"])
        command = shlex.split(receipt["swarmRunCommand"])
        self.assertEqual(command[:7], ["ao", "swarm", "run", "--plan", receipt["plan"], "--context", receipt["context"]])
        self.assertEqual(command[-2:], ["--timeout", "240"])
        self.assertIn("advisory", receipt["note"])

    def test_existing_directory_and_symlink_are_preserved(self):
        existing = self.root / "existing"
        existing.mkdir()
        marker = existing / "keep.bin"
        marker.write_bytes(b"untouched\x00contents")
        with self.assertRaises(FileExistsError):
            demo.generate(existing)
        self.assertEqual(list(existing.iterdir()), [marker])
        self.assertEqual(marker.read_bytes(), b"untouched\x00contents")
        target = self.root / "never-created"
        link = self.root / "dangling"
        link.symlink_to(target)
        with self.assertRaises(FileExistsError):
            demo.generate(link)
        self.assertTrue(link.is_symlink())
        self.assertFalse(target.exists())

    def test_failed_generation_is_retained_and_cannot_be_retried_in_place(self):
        output = self.root / "failed"
        with mock.patch.object(demo, "run_git", side_effect=RuntimeError("injected Git failure")):
            with self.assertRaisesRegex(RuntimeError, "injected Git failure"):
                demo.generate(output)
        before = {str(path.relative_to(output)): path.read_bytes() for path in output.rglob("*") if path.is_file()}
        self.assertIn("generation.log", before)
        self.assertIn("repository/CONTRACT.md", before)
        with self.assertRaises(FileExistsError):
            demo.generate(output)
        after = {str(path.relative_to(output)): path.read_bytes() for path in output.rglob("*") if path.is_file()}
        self.assertEqual(before, after)

    def test_ambient_git_redirection_is_ignored_and_options_are_explicit(self):
        foreign = self.root / "foreign.git"
        with mock.patch.dict(os.environ, {"GIT_DIR": str(foreign), "GIT_WORK_TREE": str(self.root),
                                          "GIT_CONFIG_COUNT": "1", "GIT_CONFIG_KEY_0": "core.hooksPath",
                                          "GIT_CONFIG_VALUE_0": str(self.root)}):
            output = self.root / "isolated"
            receipt = demo.generate(output, "custom-project", "custom-model", "high")
        self.assertFalse(foreign.exists())
        plan = json.loads(Path(receipt["plan"]).read_text())
        self.assertEqual(plan["projectId"], "custom-project")
        self.assertEqual(plan["tasks"][0]["agent"]["model"], "custom-model")
        self.assertEqual(plan["tasks"][0]["agent"]["effort"], "high")
        log = (output / "generation.log").read_text()
        self.assertIn("core.hooksPath=" + os.devnull, log)
        self.assertIn("commit.gpgSign=false", log)
        self.assertEqual(self.git(Path(receipt["repository"]), "status", "--porcelain=v1"), "")


if __name__ == "__main__":
    unittest.main()
