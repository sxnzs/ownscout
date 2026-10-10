#!/usr/bin/env python3
"""Behavioral tests for spec/parity/harness.py, driven through its CLI.

The harness is a program, so these tests drive it as a subprocess and read its
public seam: the exit code, the human report on stdout, and the opt-in --json
totals. That seam is what tools/mutants consumes, so a regression here would
otherwise reach the mutation table.

Run with:  python3 spec/parity/test_harness.py
Set HARNESS=/path/to/harness.py to test another copy (used to pin the regression
against the committed harness before the fix).
"""
import json
import os
import subprocess
import sys
import tempfile
import textwrap
import unittest

HERE = os.path.dirname(os.path.abspath(__file__))
HARNESS = os.environ.get("HARNESS", os.path.join(HERE, "harness.py"))

# A fake candidate: argv is <stdout> <exit-code> [path=content ...]. It writes
# each file relative to the case workdir, prints stdout, and exits.
CANDIDATE = """\
#!/usr/bin/env python3
import sys
out = sys.argv[1]
code = int(sys.argv[2])
for spec in sys.argv[3:]:
    path, _, content = spec.partition('=')
    with open(path, 'w', encoding='utf-8') as handle:
        handle.write(content)
sys.stdout.write(out)
sys.exit(code)
"""

SLEEPER = """\
#!/usr/bin/env python3
import time
time.sleep(30)
"""


def _script(directory, name, body):
    path = os.path.join(directory, name)
    with open(path, 'w', encoding='utf-8') as handle:
        handle.write(textwrap.dedent(body))
    os.chmod(path, 0o755)
    return path


class HarnessTestCase(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.dir = self.tmp.name
        self.candidate = _script(self.dir, "candidate", CANDIDATE)
        self.fixtures = os.path.join(self.dir, "fixtures")
        os.makedirs(self.fixtures)
        with open(os.path.join(self.fixtures, "marker"), "w", encoding="utf-8") as handle:
            handle.write("fixtures\n")

    def write_corpus(self, cases):
        path = os.path.join(self.dir, "corpus.json")
        with open(path, "w", encoding="utf-8") as handle:
            json.dump({"version": 1, "cases": cases}, handle)
        return path

    def run_harness(self, corpus, extra=(), bin_path=None):
        cmd = [
            sys.executable,
            HARNESS,
            "--bin",
            bin_path or self.candidate,
            "--corpus",
            corpus,
            "--fixtures",
            self.fixtures,
        ] + list(extra)
        return subprocess.run(cmd, capture_output=True, text=True, timeout=60)

    def totals(self, proc):
        self.assertIn(
            "total", proc.stdout, "harness --json did not print totals: " + proc.stderr
        )
        return json.loads(proc.stdout)


class RegressionTests(HarnessTestCase):
    def test_file_only_mismatch_is_not_reported_ok(self):
        """The pinned bug: matching exit/stdout but a wrong file printed ok."""
        corpus = self.write_corpus(
            [
                {
                    "name": "file-only",
                    "args": ["", "0", "out.txt=wrong\n"],
                    "exitCode": 0,
                    "stdout": "",
                    "files": {"out.txt": "expected\n"},
                }
            ]
        )
        proc = self.run_harness(corpus)
        self.assertEqual(proc.returncode, 1)
        self.assertNotIn("ok   file-only", proc.stdout)
        self.assertIn("out.txt mismatch", proc.stdout)
        self.assertIn("harness: 0/1 cases passed", proc.stdout)

        totals = self.totals(self.run_harness(corpus, ["--json"]))
        self.assertEqual((totals["passed"], totals["total"], totals["failed"]), (0, 1, 1))
        self.assertFalse(totals["cases"][0]["passed"])
        self.assertTrue(totals["cases"][0]["messages"])

    def test_multiple_file_mismatches_count_as_one_case(self):
        corpus = self.write_corpus(
            [
                {
                    "name": "two-files",
                    "args": ["", "0", "a.txt=x\n", "b.txt=y\n"],
                    "exitCode": 0,
                    "stdout": "",
                    "files": {"a.txt": "A\n", "b.txt": "B\n"},
                }
            ]
        )
        human = self.run_harness(corpus)
        self.assertEqual(human.returncode, 1)
        self.assertIn("a.txt mismatch", human.stdout)
        self.assertIn("b.txt mismatch", human.stdout)
        # The denominator is the corpus size and the numerator is cases passed,
        # never the number of mismatches.
        self.assertIn("harness: 0/1 cases passed", human.stdout)
        self.assertNotIn("ok   two-files", human.stdout)

        totals = self.totals(self.run_harness(corpus, ["--json"]))
        self.assertEqual((totals["passed"], totals["total"]), (0, 1))


class OutcomeTests(HarnessTestCase):
    def test_timeout_is_a_failing_case(self):
        sleeper = _script(self.dir, "sleeper", SLEEPER)
        corpus = self.write_corpus(
            [
                {
                    "name": "slow",
                    "args": [],
                    "exitCode": 0,
                    "stdout": "",
                }
            ]
        )
        proc = self.run_harness(corpus, ["--timeout", "1", "--json"], bin_path=sleeper)
        self.assertEqual(proc.returncode, 1)
        totals = self.totals(proc)
        self.assertEqual((totals["passed"], totals["total"]), (0, 1))
        self.assertIn("timed out", totals["cases"][0]["messages"][0])

    def test_ok_lines_and_totals_only_for_passing_cases(self):
        corpus = self.write_corpus(
            [
                {"name": "good", "args": ["fine\n", "0"], "exitCode": 0, "stdout": "fine\n"},
                {
                    "name": "bad",
                    "args": ["", "0", "out.txt=wrong\n"],
                    "exitCode": 0,
                    "stdout": "",
                    "files": {"out.txt": "expected\n"},
                },
            ]
        )
        human = self.run_harness(corpus, ["--verbose"])
        self.assertEqual(human.returncode, 1)
        self.assertEqual(human.stdout.count("ok   "), 1)
        self.assertIn("ok   good", human.stdout)
        self.assertNotIn("ok   bad", human.stdout)
        self.assertIn("harness: 1/2 cases passed", human.stdout)

        totals = self.totals(self.run_harness(corpus, ["--json"]))
        self.assertEqual((totals["passed"], totals["total"], totals["failed"]), (1, 2, 1))
        self.assertEqual(
            [c["passed"] for c in totals["cases"]], [True, False]
        )

    def test_all_pass_exits_zero(self):
        corpus = self.write_corpus(
            [
                {"name": "one", "args": ["a\n", "0"], "exitCode": 0, "stdout": "a\n"},
                {"name": "two", "args": ["b", "3"], "exitCode": 3, "stdout": "b"},
            ]
        )
        proc = self.run_harness(corpus)
        self.assertEqual(proc.returncode, 0)
        self.assertIn("harness: 2/2 cases passed", proc.stdout)
        totals = self.totals(self.run_harness(corpus, ["--json"]))
        self.assertEqual((totals["passed"], totals["total"], totals["failed"]), (2, 2, 0))

    def test_json_is_opt_in_and_default_stays_human(self):
        corpus = self.write_corpus(
            [{"name": "one", "args": ["a\n", "0"], "exitCode": 0, "stdout": "a\n"}]
        )
        human = self.run_harness(corpus)
        with self.assertRaises(json.JSONDecodeError):
            json.loads(human.stdout)
        self.assertIn("harness: 1/1 cases passed", human.stdout)

    def test_missing_binary_is_infrastructure_exit_two(self):
        corpus = self.write_corpus([])
        proc = self.run_harness(corpus, bin_path=os.path.join(self.dir, "does-not-exist"))
        self.assertEqual(proc.returncode, 2)
        self.assertIn("missing or not executable", proc.stdout)
        self.assertEqual(proc.stderr, "")
        proc = self.run_harness(corpus, ["--json"], bin_path=os.path.join(self.dir, "does-not-exist"))
        self.assertEqual(proc.returncode, 2)
        self.assertEqual(proc.stdout, "")
        self.assertIn("missing or not executable", proc.stderr)


if __name__ == "__main__":
    unittest.main(verbosity=2)
