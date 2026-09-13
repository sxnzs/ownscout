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


def main():
    here = os.path.dirname(os.path.abspath(__file__))
    parser = argparse.ArgumentParser()
    parser.add_argument("--bin", required=True, help="candidate ownscout binary")
    parser.add_argument("--corpus", default=os.path.join(here, "corpus.json"))
    parser.add_argument("--verbose", action="store_true")
    args = parser.parse_args()

    with open(args.corpus, encoding="utf-8") as handle:
        corpus = json.load(handle)
    source_fixtures = os.path.join(here, "fixtures")
    binary = os.path.abspath(args.bin)
    if not os.path.exists(binary) or not os.access(binary, os.X_OK):
        print("harness: binary is missing or not executable: " + binary)
        return 2

    failures = []
    for case in corpus["cases"]:
        with tempfile.TemporaryDirectory() as workdir:
            shutil.copytree(source_fixtures, os.path.join(workdir, "fixtures"), symlinks=True)
            repo = os.path.join(workdir, "fixtures", "repo")
            try:
                proc = subprocess.run(
                    [binary] + case["args"],
                    cwd=workdir,
                    capture_output=True,
                    timeout=30,
                )
            except subprocess.TimeoutExpired:
                failures.append(case["name"] + ": timed out")
                continue
            stdout = (proc.stdout + proc.stderr).decode("utf-8", "replace")
            stdout = normalize(stdout, workdir, repo, binary)
            if proc.returncode != case["exitCode"]:
                failures.append(
                    "{}: exit {} != {}".format(case["name"], proc.returncode, case["exitCode"])
                )
                continue
            if stdout != case["stdout"]:
                failures.append(
                    "{}: stdout mismatch\n--- want ---\n{}--- got ---\n{}".format(
                        case["name"], case["stdout"], stdout
                    )
                )
                continue
            for name, want in case.get("files", {}).items():
                path = os.path.join(workdir, name)
                got = open(path, encoding="utf-8").read() if os.path.exists(path) else ""
                got = normalize(got, workdir, repo, binary)
                if got != want:
                    failures.append(
                        "{}: {} mismatch\n--- want ---\n{}--- got ---\n{}".format(
                            case["name"], name, want, got
                        )
                    )
        if args.verbose:
            print("ok   " + case["name"])

    total = len(corpus["cases"])
    if failures:
        print("\n".join(failures))
        print("harness: {}/{} cases passed".format(total - len(failures), total))
        return 1
    print("harness: {}/{} cases passed".format(total, total))
    return 0


if __name__ == "__main__":
    sys.exit(main())
