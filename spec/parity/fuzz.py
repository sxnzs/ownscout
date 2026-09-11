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
import shutil
import subprocess
import sys
import tempfile

HERE = os.path.dirname(os.path.abspath(__file__))
FIXTURES = os.path.join(HERE, "fixtures")

PACKET_CASES = [
    ["contract", "validate", "--packet", "fixtures/mutated.json"],
    ["contract", "validate", "--packet", "fixtures/mutated.json", "--json"],
    ["evidence", "verify", "--repo", "fixtures/repo", "--packet", "fixtures/mutated.json", "--json"],
]
ENVELOPE_CASES = [
    ["node", "verify", "--repo", "fixtures/repo", "--packet", "fixtures/packet-valid.json",
     "--envelope", "fixtures/mutated.json", "--ledger", "ledger.jsonl", "--json"],
]


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

    rng = random.Random(args.seed)
    print("fuzz: seed={} iterations={}".format(args.seed, args.iterations))
    divergences = 0
    for iteration in range(args.iterations):
        source, cases = (("packet-valid.json", PACKET_CASES) if rng.randrange(2) == 0
                         else ("envelope-valid.json", ENVELOPE_CASES))
        original = open(os.path.join(FIXTURES, source), "rb").read()
        data = original
        for _ in range(rng.randrange(1, 4)):
            data = mutate(data, rng)
        with tempfile.TemporaryDirectory() as workdir:
            shutil.copytree(FIXTURES, os.path.join(workdir, "fixtures"))
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
        if args.verbose and iteration % 25 == 0:
            print("fuzz: {} iterations".format(iteration))
    if divergences:
        print("fuzz: DIVERGENCES FOUND ({} cases)".format(divergences))
        return 1
    print("fuzz: no divergence in {} iterations".format(args.iterations))
    return 0


if __name__ == "__main__":
    sys.exit(main())
