#!/usr/bin/env python3
"""Run a candidate OwnScout binary against the recorded parity corpus."""
import argparse
import json
import os
import shutil
import subprocess
import sys
import tempfile


def normalize(text, workdir, repo, binary):
    # Resolve symlinked temp paths (macOS /var -> /private/var) so both the
    # literal and the resolved forms map to the recorded placeholders.
    for needle, replacement in (
        (os.path.realpath(repo), "{{REPO}}"),
        (repo, "{{REPO}}"),
        (os.path.realpath(workdir), "{{DIR}}"),
        (workdir, "{{DIR}}"),
        (os.path.realpath(binary), "{{BIN}}"),
        (binary, "{{BIN}}"),
    ):
        text = text.replace(needle, replacement)
    return text


def run_case(case, source_fixtures, binary, timeout):
    """Run one corpus case in a throwaway workdir; return its failure messages.

    The list is the authoritative result for the case: empty means it passed, any
    entry means it failed. A single case can report several mismatches (several
    expected files, for instance) but is still one case, so callers count cases,
    never messages. This is what keeps a case that only mismatches a file from
    being counted as "ok" alongside its own failure.
    """
    failures = []
    with tempfile.TemporaryDirectory() as workdir:
        shutil.copytree(source_fixtures, os.path.join(workdir, "fixtures"), symlinks=True)
        repo = os.path.join(workdir, "fixtures", "repo")
        try:
            proc = subprocess.run(
                [binary] + case["args"],
                cwd=workdir,
                capture_output=True,
                timeout=timeout,
            )
        except subprocess.TimeoutExpired:
            return [case["name"] + ": timed out"]
        stdout = (proc.stdout + proc.stderr).decode("utf-8", "replace")
        stdout = normalize(stdout, workdir, repo, binary)
        if proc.returncode != case["exitCode"]:
            failures.append(
                "{}: exit {} != {}".format(case["name"], proc.returncode, case["exitCode"])
            )
            return failures
        if stdout != case["stdout"]:
            failures.append(
                "{}: stdout mismatch\n--- want ---\n{}--- got ---\n{}".format(
                    case["name"], case["stdout"], stdout
                )
            )
            return failures
        for name, want in case.get("files", {}).items():
            path = os.path.join(workdir, name)
            got = ""
            if os.path.exists(path):
                with open(path, encoding="utf-8") as handle:
                    got = handle.read()
            got = normalize(got, workdir, repo, binary)
            if got != want:
                failures.append(
                    "{}: {} mismatch\n--- want ---\n{}--- got ---\n{}".format(
                        case["name"], name, want, got
                    )
                )
    return failures


def main():
    here = os.path.dirname(os.path.abspath(__file__))
    parser = argparse.ArgumentParser()
    parser.add_argument("--bin", required=True, help="candidate ownscout binary")
    parser.add_argument("--corpus", default=os.path.join(here, "corpus.json"))
    parser.add_argument(
        "--fixtures",
        default=os.path.join(here, "fixtures"),
        help="fixture tree copied into each case's workdir (test seam)",
    )
    parser.add_argument(
        "--timeout",
        type=float,
        default=30,
        help="per-case timeout in seconds (test seam)",
    )
    parser.add_argument("--verbose", action="store_true")
    # Opt-in machine-readable totals for callers such as tools/mutants. The
    # default remains the human report; --json replaces it so nothing has to
    # parse text markers out of a mixed stream.
    parser.add_argument("--json", action="store_true")
    args = parser.parse_args()

    with open(args.corpus, encoding="utf-8") as handle:
        corpus = json.load(handle)
    binary = os.path.abspath(args.bin)
    if not os.path.exists(binary) or not os.access(binary, os.X_OK):
        print("harness: binary is missing or not executable: " + binary,
              file=sys.stderr if args.json else sys.stdout)
        return 2

    results = []
    failures = []
    for case in corpus["cases"]:
        case_failures = run_case(case, args.fixtures, binary, args.timeout)
        results.append(
            {"name": case["name"], "passed": not case_failures, "messages": case_failures}
        )
        if case_failures:
            failures.extend(case_failures)
        elif args.verbose and not args.json:
            print("ok   " + case["name"])

    total = len(corpus["cases"])
    passed = sum(1 for result in results if result["passed"])

    if args.json:
        print(
            json.dumps(
                {
                    "total": total,
                    "passed": passed,
                    "failed": total - passed,
                    "cases": results,
                }
            )
        )
    else:
        if failures:
            print("\n".join(failures))
        print("harness: {}/{} cases passed".format(passed, total))

    return 0 if passed == total else 1


if __name__ == "__main__":
    sys.exit(main())
