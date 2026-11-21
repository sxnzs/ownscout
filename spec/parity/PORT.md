# OwnScout behaviour contract

`spec/parity/corpus.json` (35 cases) and `spec/parity/corpus-edge.json` (249
cases) are the frozen behaviour spec for `ownscout`. `spec/parity/harness.py`
replays each case against a real binary, and a case passes only when the exit
code, the combined output and any recorded files match byte for byte. A failing
case names the exact bytes: fix the binary, not the corpus.

## What the corpora pin

- **Output stream.** The harness concatenates stdout and stderr, then compares
  the result to the recorded text. Wording, ordering and trailing newlines are
  all part of the contract.
- **Exit codes.** `0` success, valid packet, or verified evidence. `1` contract
  failure, evidence failure, or node graph failure after a successful ledger
  append. `2` usage error, malformed input, missing file, repository or ledger
  I/O, or ledger append failure.
- **Ledger bytes.** Cases that append or rotate carry a `files` map. Each entry
  is read from the case workdir after the run and compared exactly. The ledger
  has no timestamps, so its bytes are reproducible.
- **JSON mode.** One line, one trailing newline, exactly five fields in the
  order `command`, `ok`, `summary`, `details`, `next_action`. Strings
  HTML-escape `<`, `>` and `&` as `\u003c`, `\u003e` and `\u0026`.
- **Human mode.** Recorded wording and the `Next action:` line.
- **Placeholders.** Expected text stores `{{DIR}}` (the case workdir), `{{REPO}}`
  (`{{DIR}}/fixtures/repo`) and `{{BIN}}` (the absolute candidate binary path).
  The harness replaces the literal and realpath forms before comparing, which
  absorbs the macOS `/var` to `/private/var` symlink.

## Running the harness

```
python3 spec/parity/harness.py --bin target/release/ownscout
python3 spec/parity/harness.py --corpus spec/parity/corpus-edge.json --bin target/release/ownscout
```

Each case runs in a throwaway directory with a fresh copy of
`spec/parity/fixtures/`, so cases cannot leak state into each other. `--verbose`
prints passing case names. `--json` emits machine-readable totals and per-case
results for callers that need to parse them.

## Commands pinned

- `ownscout doctor`
- `ownscout version` prints `ownscout 0.1.0`
- `ownscout contract validate --packet <file> [--json]`
- `ownscout evidence verify --repo <dir> --packet <file> [--relocate] [--json]`
- `ownscout node verify --repo <dir> --packet <file> --envelope <file> --ledger <file> [--relocate] [--json]`
- `ownscout node bind --packet <file> [--json]`
- `ownscout ledger verify --ledger <file> [--json]`
- `ownscout ledger rotate --ledger <file> [--json]`
- `--help` at the root and for each command, plus `ownscout <cmd> --help`

## Behaviour to preserve

- **Strict packet decoding** (`src/packet.rs`), shared by every command, not
  only the `node` paths: exactly one JSON object, exact case-sensitive field
  names, duplicate keys rejected, unknown fields rejected, no trailing JSON,
  1 MiB inclusive limit, valid UTF-8. Decode failures report the generic
  `strict packet decoding failed` detail, so no packet contents or decoder
  internals leak into output.
- **Contract validation** (`src/contract.rs`) and the outcome to action table in
  `specs/packet-v1.md`. Violation ordering and the human and JSON rendering are
  part of the contract.
- **Evidence verification** (`src/evidence.rs`): resolve the repository root,
  confine evidence paths inside it (reject `..` and symlink escape), re-hash the
  current working-tree file, normalize line endings, and compare the expected
  content hash. The packet is input only; `verifier_status` is never trusted or
  rewritten.
- **Raw evidence bytes.** Hashing reads raw file bytes. CRLF pairs count as one
  terminator, a single empty selected line hashes the empty string, evidence
  files have no size limit, and range errors report the real line count.
- **Anchor re-resolution** (`src/evidence.rs`): with `--relocate`, a failed span
  is searched for a window of the same line count whose fingerprint matches,
  probing nearest-first from the cited start (lower line number first on a tie),
  with the origin clamped into the range of windows that fit and an 8 MiB cap on
  the total window bytes hashed. It is diagnostic only: status, counters, exit
  code and ledger bytes are unchanged, and a moved span stays `failed`. The
  three clauses (relocates, not found elsewhere, stopped on the byte budget) are
  part of the contract, and `--relocate` is rejected by every other subcommand.
- **node-envelope-v1** (`src/node.rs`): the same strict-parse rules, a canonical
  packet binding digest, graph validation (duplicate ids and references,
  missing or self dependencies, cycles, size limits), deterministic topological
  evaluation with lexicographic tie-breaking, and an append-only JSONL ledger
  outside the repository with SHA-256 hash chaining (`src/ledger.rs`). The
  1 MiB ledger cap is a rotation point: a full ledger surfaces as `ledger is
  full` with the archive-aside recovery text, and `ledger verify` audits any
  file read-only (non-regular paths rejected before open).
- **node bind** (`src/cli.rs`): the canonical packet binding digest, computed at
  the shared strict boundary. Contract violations exit `1`; decode failures exit
  `2` with the generic detail.
- **Path safety**: repository and ledger paths are resolved. The append path and
  `ledger rotate` reject a symlinked ancestor; the read-only `ledger verify`
  follows one, and a symlink as the final path is rejected everywhere. Inputs
  are size-bounded.
- **Standard library only.** The crate is deterministic and free of network,
  daemon, or repository mutation.

## Deliberate behaviour change

A deliberate output change means editing the corpus JSON in the same commit:
update the affected case (or add one), keep `fixtures/` consistent, and run
`./scripts/verify` until both corpora pass. That corpus edit needs owner review.
Never weaken a case so a failing binary passes.
