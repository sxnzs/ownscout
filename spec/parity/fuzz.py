#!/usr/bin/env python3
"""Differential fuzzer for OwnScout ports.

Runs a reference binary and a candidate binary over randomly mutated copies of
the parity fixtures, then reports any divergence in exit code or output. The
seed is printed so every run is reproducible.

    python3 spec/parity/fuzz.py --candidate ports/zig/zig-out/bin/ownscout
"""
import argparse
import json
import os
import random
import re
import shutil
import subprocess
import sys
import tempfile

HERE = os.path.dirname(os.path.abspath(__file__))
REPO = os.path.dirname(os.path.dirname(HERE))  # spec/parity -> repo root
FIXTURES = os.path.join(HERE, "fixtures")


def newest_source_mtime():
    """The newest mtime among the Go sources the reference is built from."""
    newest = 0.0
    for base in ("internal", "cmd"):
        for root, _, files in os.walk(os.path.join(REPO, base)):
            for name in files:
                if name.endswith(".go"):
                    newest = max(newest, os.path.getmtime(os.path.join(root, name)))
    for name in ("go.mod", "go.sum"):
        path = os.path.join(REPO, name)
        if os.path.exists(path):
            newest = max(newest, os.path.getmtime(path))
    return newest

PACKET_CASES = [
    ["contract", "validate", "--packet", "fixtures/mutated.json"],
    ["contract", "validate", "--packet", "fixtures/mutated.json", "--json"],
    ["evidence", "verify", "--repo", "fixtures/repo", "--packet", "fixtures/mutated.json", "--json"],
    # bind shares the strict decode boundary; a port that kept leniency on
    # only this command would otherwise slip the packet-mutation phase.
    ["node", "bind", "--packet", "fixtures/mutated.json", "--json"],
]
ENVELOPE_CASES = [
    ["node", "verify", "--repo", "fixtures/repo", "--packet", "fixtures/packet-valid.json",
     "--envelope", "fixtures/mutated.json", "--ledger", "ledger.jsonl", "--json"],
]
# Ledger coverage. The envelope case above always starts from a fresh ledger -
# the loop removes it before every run - so nothing ever validated an EXISTING
# one. That is how a port which silently accepted a forged ledger passed every
# seed; the gap was in the fuzzer, not only in the port. These cases seed a
# mutated ledger and then verify against it.
LEDGER_CASES = [
    ["ledger", "verify", "--ledger", "fixtures/mutated.jsonl"],
    ["ledger", "verify", "--ledger", "fixtures/mutated.jsonl", "--json"],
    # rotate validates before renaming — a port that skips validation would
    # rotate broken chains the reference refuses to touch. run_isolated gives
    # each binary a fresh workdir, so the rename is safe to exercise.
    ["ledger", "rotate", "--ledger", "fixtures/mutated.jsonl", "--json"],
    ["node", "verify", "--repo", "fixtures/repo", "--packet", "fixtures/packet-valid.json",
     "--envelope", "fixtures/envelope-valid.json", "--ledger", "fixtures/mutated.jsonl", "--json"],
]
LEDGER_SEED = os.path.join(FIXTURES, "edge", "ledger-seed.jsonl")
LEDGER_TARGET = "fixtures/mutated.jsonl"
# Every run of the ledger phase is independent, so it is sampled rather than run
# every iteration: it costs three extra cases in two workdirs.
LEDGER_EVERY = 4


def flip_byte(data, rng):
    index = rng.randrange(len(data))
    changed = bytearray(data)
    changed[index] ^= 1 << rng.randrange(8)
    return bytes(changed)


def mutate_bytes(data, rng):
    if not data:
        return data
    kind = rng.randrange(7)
    if kind == 0:
        return flip_byte(data, rng)
    if kind == 1:
        index = rng.randrange(len(data))
        return data[:index] + data[index + 1:]
    if kind == 2:
        index = rng.randrange(len(data) + 1)
        return data[:index] + bytes([rng.randrange(256)]) + data[index:]
    if kind == 3:
        return data[:rng.randrange(len(data) + 1)]
    if kind == 4:
        start = rng.randrange(len(data))
        end = min(len(data), start + rng.randrange(1, 48))
        return data[:end] + data[start:end] + data[end:]
    if kind == 5:
        index = rng.randrange(len(data))
        pool = b'{}[]",:0123456789.eE+- \t\nnulltrue'
        changed = bytearray(data)
        changed[index] = pool[rng.randrange(len(pool))]
        return bytes(changed)
    index = rng.randrange(len(data))
    other = rng.randrange(len(data))
    changed = bytearray(data)
    changed[index], changed[other] = changed[other], changed[index]
    return bytes(changed)


def walk(node, path=()):
    yield path, node
    if isinstance(node, dict):
        for key, value in node.items():
            yield from walk(value, path + (key,))
    elif isinstance(node, list):
        for index, value in enumerate(node):
            yield from walk(value, path + (index,))


def set_path(root, path, value):
    node = root
    for step in path[:-1]:
        node = node[step]
    node[path[-1]] = value


def mutate_json(data, rng):
    try:
        root = json.loads(data)
    except Exception:
        return None
    entries = list(walk(root))
    if not entries:
        return None
    path, value = rng.choice(entries)
    if path:
        choice = rng.randrange(4)
        if choice == 0:
            set_path(root, path, None)
        elif choice == 1:
            set_path(root, path, [] if not isinstance(value, list) else {})
        elif choice == 2:
            set_path(root, path, "x" * rng.randrange(1, 8))
        else:
            set_path(root, path, rng.randrange(0, 1000000))
    else:
        root = value
    if rng.randrange(4) == 0:
        text = json.dumps(root)
        return text.encode()
    return json.dumps(root).encode()


def mutate(data, rng):
    if rng.randrange(2) == 0:
        changed = mutate_json(data, rng)
        if changed is not None:
            return changed
    return mutate_bytes(data, rng)


def wire_shapes(original):
    """Return the wire shapes that random byte mutation rarely produces.

    A byte-level fuzzer almost never creates a duplicate key, a case-folded
    name, a UTF-8 BOM or a 1 MiB document, yet those are exactly the shapes that
    have diverged between the reference and the ports. The shapes are derived
    from whichever key the fixture happens to hold first, so the same function
    covers the packet and the envelope. A shape that does not change the input
    is dropped rather than replayed unchanged, so no run is spent proving
    nothing.
    """
    text = original.decode("utf-8")
    shapes = {
        "trailing-json": text + "{}",
        "bom": "\ufeff" + text,
        "oversize": text + " " + "x" * (1 << 20),
        "truncated": text[: len(text) // 2],
    }
    first = re.search(r'"([A-Za-z_][A-Za-z0-9_]*)":', text)
    if first:
        key = first.group(1)
        shapes["duplicate-key"] = (
            text[: first.start()] + '"{}": null, '.format(key) + text[first.start():])
        shapes["case-folding"] = text[: first.start(1)] + key.upper() + text[first.end(1):]
        shapes["unknown-field"] = (
            text[: first.start()] + '"extra_field": "DO_NOT_PRINT", ' + text[first.start():])
    nulled, count = re.subn(r'("[A-Za-z_][A-Za-z0-9_]*": )"[^"]*"', r"\1null", text, count=1)
    if count:
        shapes["null-value"] = nulled
    return {name: data.encode() for name, data in shapes.items() if data != text}


def replay(reference, candidate, cases, data, workdir, label):
    """Run every case against both binaries and report the differences."""
    findings = []
    with open(os.path.join(workdir, "fixtures", "mutated.json"), "wb") as handle:
        handle.write(data)
    repo = os.path.join(workdir, "fixtures", "repo")
    for case in cases:
        ledger = os.path.join(workdir, "ledger.jsonl")
        expected = None
        for binary in (reference, candidate):
            if os.path.exists(ledger):
                os.remove(ledger)
            code, text = run(binary, case, workdir)
            text = normalize(text, workdir, repo, binary)
            if binary is reference:
                expected = (code, text)
            elif (code, text) != expected:
                findings.append(
                    "DIVERGENCE {}\n  case:     {}\n  exit:     {} != {}\n  {}\n"
                    "  input:    {!r}".format(
                        label, " ".join(case), code, expected[0],
                        first_difference(expected[1], text).strip(), data[:120]))
    return findings


def first_difference(expected, actual):
    limit = min(len(expected), len(actual))
    index = next((i for i in range(limit) if expected[i] != actual[i]), limit)
    start = max(0, index - 30)
    return "at {}:\n    expected ...{!r}\n    actual   ...{!r}".format(
        index, expected[start:index + 50], actual[start:index + 50])


def normalize(text, workdir, repo, binary):
    for needle, replacement in (
        (os.path.realpath(repo), "{{REPO}}"),
        (repo, "{{REPO}}"),
        (os.path.realpath(workdir), "{{DIR}}"),
        (workdir, "{{DIR}}"),
        (binary, "{{BIN}}"),
        (os.path.realpath(binary), "{{BIN}}"),
    ):
        text = text.replace(needle, replacement)
    return text


def run(binary, args, workdir):
    try:
        proc = subprocess.run([binary] + args, cwd=workdir, capture_output=True, timeout=30)
    except subprocess.TimeoutExpired:
        return "timeout", ""
    return proc.returncode, (proc.stdout + proc.stderr).decode("utf-8", "replace")


def run_isolated(binary, case, ledger_data):
    """Run one case in a fresh fixture copy, with the ledger seeded to ledger_data.

    The ledger phase cannot share a workdir between the reference and the
    candidate: a run that accepts the ledger appends to it, which would change
    the input the second binary sees and manufacture a divergence.
    """
    with tempfile.TemporaryDirectory() as workdir:
        shutil.copytree(FIXTURES, os.path.join(workdir, "fixtures"), symlinks=True)
        with open(os.path.join(workdir, LEDGER_TARGET), "wb") as handle:
            handle.write(ledger_data)
        repo = os.path.join(workdir, "fixtures", "repo")
        code, text = run(binary, case, workdir)
        return code, normalize(text, workdir, repo, binary)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--candidate", required=True)
    parser.add_argument("--reference", default=os.path.join(HERE, "reference-ownscout"))
    parser.add_argument("--iterations", type=int, default=200)
    parser.add_argument("--seed", type=int, default=1)
    parser.add_argument("--verbose", action="store_true")
    args = parser.parse_args()

    candidate = os.path.abspath(args.candidate)
    reference = os.path.abspath(args.reference)
    for binary, label in ((candidate, "candidate"), (reference, "reference")):
        if not os.path.exists(binary) or not os.access(binary, os.X_OK):
            print("fuzz: {} binary is missing or not executable: {}".format(label, binary))
            return 2
    # A stale reference reports divergences the candidate does not have, which
    # costs a lane real time and can send it chasing a bug that is not there.
    # make fuzz-sweep rebuilds both binaries first; a bare call does not.
    if os.path.getmtime(reference) < newest_source_mtime():
        print("fuzz: WARNING: the reference binary is older than the Go sources; "
              "run `make reference` first or every divergence below may be false")

    rng = random.Random(args.seed)
    # The ledger phase draws from its own stream, so adding it left every
    # previously documented seed result reproducible.
    ledger_rng = random.Random(args.seed * 7919 + 13)
    print("fuzz: seed={} iterations={}".format(args.seed, args.iterations))
    divergences = 0

    # Deterministic wire shapes first. A divergence here is a hard defect, so
    # the phase stops before the random sweep rather than adding noise to it.
    for source, cases in (("packet-valid.json", PACKET_CASES),
                          ("envelope-valid.json", ENVELOPE_CASES)):
        original = open(os.path.join(FIXTURES, source), "rb").read()
        for name, data in sorted(wire_shapes(original).items()):
            with tempfile.TemporaryDirectory() as workdir:
                shutil.copytree(FIXTURES, os.path.join(workdir, "fixtures"), symlinks=True)
                findings = replay(reference, candidate, cases, data, workdir,
                                  "wire-shape {} on {}".format(name, source))
            for finding in findings:
                print(finding)
            divergences += len(findings)
    if divergences:
        print("fuzz: {} wire-shape divergence(s)".format(divergences))
        return 1
    print("fuzz: wire shapes replay clean")

    for iteration in range(args.iterations):
        source, cases = (("packet-valid.json", PACKET_CASES) if rng.randrange(2) == 0
                         else ("envelope-valid.json", ENVELOPE_CASES))
        original = open(os.path.join(FIXTURES, source), "rb").read()
        data = original
        for _ in range(rng.randrange(1, 4)):
            data = mutate(data, rng)
        with tempfile.TemporaryDirectory() as workdir:
            shutil.copytree(FIXTURES, os.path.join(workdir, "fixtures"), symlinks=True)
            with open(os.path.join(workdir, "fixtures", "mutated.json"), "wb") as handle:
                handle.write(data)
            repo = os.path.join(workdir, "fixtures", "repo")
            for case in cases:
                ledger = os.path.join(workdir, "ledger.jsonl")
                for binary in (reference, candidate):
                    if os.path.exists(ledger):
                        os.remove(ledger)
                    code, text = run(binary, case, workdir)
                    text = normalize(text, workdir, repo, binary)
                    if binary is reference:
                        expected = (code, text)
                    elif (code, text) != expected:
                        divergences += 1
                        print("DIVERGENCE iteration={} case={}".format(iteration, " ".join(case)))
                        print("  exit:     {} != {}".format(code, expected[0]))
                        print("  " + first_difference(expected[1], text))
                        print("  input:    {!r}".format(data[:120]))
                if divergences >= 10:
                    print("fuzz: stopping after 10 divergences")
                    print("fuzz: DIVERGENCES FOUND ({} cases)".format(divergences))
                    return 1
        if iteration % LEDGER_EVERY == 0:
            ledger_data = open(LEDGER_SEED, "rb").read()
            for _ in range(ledger_rng.randrange(1, 4)):
                ledger_data = mutate(ledger_data, ledger_rng)
            for case in LEDGER_CASES:
                expected = None
                for binary in (reference, candidate):
                    code, text = run_isolated(binary, case, ledger_data)
                    if binary is reference:
                        expected = (code, text)
                    elif (code, text) != expected:
                        divergences += 1
                        print("DIVERGENCE iteration={} case={}".format(iteration, " ".join(case)))
                        print("  exit:     {} != {}".format(code, expected[0]))
                        print("  " + first_difference(expected[1], text))
                        print("  input:    {!r}".format(ledger_data[:120]))
                if divergences >= 10:
                    print("fuzz: stopping after 10 divergences")
                    print("fuzz: DIVERGENCES FOUND ({} cases)".format(divergences))
                    return 1
        if args.verbose and iteration % 25 == 0:
            print("fuzz: {} iterations".format(iteration))
    if divergences:
        print("fuzz: DIVERGENCES FOUND ({} cases)".format(divergences))
        return 1
    print("fuzz: no divergence in {} iterations".format(args.iterations))
    return 0


if __name__ == "__main__":
    sys.exit(main())
