# OwnScout port contract

This directory records the exact behavior a port must reproduce. The Go
implementation under `internal/` is the reference; `corpus.json` is the
recorded trace corpus. Read the Go source — it is the specification.

## What to build

A native implementation of the OwnScout CLI under `ports/<lang>/`, producing a
binary with the same commands, the same stdout, and the same exit codes.

| Port | Binary (exact path) | Build | Tests |
|---|---|---|---|
| TypeScript | `ports/ts/bin/ownscout` | executable wrapper; Node 24 runs `.ts` directly | `cd ports/ts && node --test` |
| Zig | `ports/zig/zig-out/bin/ownscout` | `cd ports/zig && zig build` | `cd ports/zig && zig build test` |
| Rust | `ports/rust/target/release/ownscout` | `cd ports/rust && cargo build --release` | `cd ports/rust && cargo test` |

`spec/parity/verify-all.sh` expects exactly these paths and commands.

Commands:

- `ownscout doctor`
- `ownscout version` -> `ownscout 0.1.0`
- `ownscout contract validate --packet <file> [--json]`
- `ownscout evidence verify --repo <dir> --packet <file> [--relocate] [--json]`
- `ownscout node verify --repo <dir> --packet <file> --envelope <file> --ledger <file> [--relocate] [--json]`
- `ownscout node bind --packet <file> [--json]`
- `ownscout ledger verify --ledger <file> [--json]`
- `ownscout ledger rotate --ledger <file> [--json]`
- `--help` at the root and for each command, plus `ownscout <cmd> --help`

## Exit codes

- `0` success, valid packet, or verified evidence
- `1` contract failure, evidence failure, or node graph failure after append
- `2` usage error, malformed input, missing file, or repository/ledger I/O

## The trace corpus

```
python3 spec/parity/harness.py --bin ports/<lang>/<binary>
```

runs all 28 recorded cases. Expected stdout is stored with placeholders that the
harness substitutes:

- `{{DIR}}`  — the working directory the case ran in
- `{{REPO}}` — `{{DIR}}/fixtures/repo`
- `{{BIN}}`  — the candidate binary path

Every case must match byte-for-byte: trailing newlines included, and JSON mode
must be exactly one line (one trailing `\n`) with exactly five fields:
`command`, `ok`, `summary`, `details`, `next_action`. Human mode keeps the
recorded wording and "Next action:" line.

## JSON encoder details

JSON mode is produced by Go's `encoding/json`, which HTML-escapes `<`, `>` and
`&` inside strings as `\u003c`, `\u003e` and `\u0026` (see the recorded
`details` values in `corpus-edge.json`). Field order is fixed:
`command`, `ok`, `summary`, `details`, `next_action`. Reproduce both exactly.

## Edge corpus

`corpus-edge.json` (247 cases) is a hardening set: every contract-testdata
fixture, the subcommand surface, synthesized node-envelope graph failures, raw
JSON parse failures, a graph whose evidence verification fails (exit 1, with a
ledger), an in-repository ledger rejection, the raw-byte evidence shapes
(`fixtures/edge/packet-evidence-shape-*.json` against
`fixtures/repo/binary.txt`, `blank.txt` and `oversize.txt`): hashing reads raw
file bytes, CRLF pairs are one terminator, a single empty selected line hashes
the empty string, evidence files have no size limit, and range errors report
the real line count, and anchor re-resolution (`evidence verify --relocate`,
`fixtures/edge/packet-evidence-relocate-*.json`): a moved span names its new
lines and signed shift, absent content is reported only once every fitting
window has been probed, a file too large to cover reports that the search
stopped on its byte budget, the same packet without the flag is byte-identical
to the pre-relocation wording, and `--relocate` is rejected by every other
subcommand, and the node-verify usage surface: the four paths are reported
missing in a fixed order (repo, packet, envelope, ledger) and a usage error is
rendered as a structured result under `--json`. That last group exists because
the Rust port was found diverging on both, having passed the previous corpus.
Run it with the same harness:

```
python3 spec/parity/harness.py --corpus spec/parity/corpus-edge.json --bin <binary>
```

## Trace corpus validation

The corpus was mutation-tested against five deliberately broken reference builds:

| Mutation | Base corpus | Edge corpus |
|---|---|---|
| `SetEscapeHTML(false)` in the JSON result encoder | 33/35 | 205/247 |
| `next_action` renamed to `nextAction` | 25/35 | 123/247 |
| final newline always appended to the hashed evidence range | 35/35 | 245/247 |
| `\r` stripped without its `\n` in the relocation path | 35/35 | 245/247 |
| `strings.ToLower` dropped from the expected-hash comparison | 33/35 | 247/247 |
| unmutated reference | 35/35 | 247/247 |

Each subtle divergence is caught, so a passing harness is meaningful rather
than vacuous. The third mutation is the single-empty-line rule above: the
shape cases exist because all three ports shipped that exact bug while passing
the much smaller corpus of the time. The fourth mutation is the mirror image -
stripping a `\r` without consuming its `\n` - and it is invisible to the base
corpus, which is the point of keeping the two corpora separate.

The table is measured, not transcribed. `make mutants` stages a copy of the
module, applies each mutation to that copy, builds it, and replays both corpora;
`make mutants-check` re-measures and fails when the table above no longer
matches. `make mutants` rewrites only the sentence and table shown here, so the
prose below is hand-written and the numbers above are not.

## Behavior to preserve (read the Go source)

- **packet-v1 strict decoding** (`internal/nodepacket/decode.go`) — shared by
  every command, not only `node` paths: exactly one JSON object, exact
  case-sensitive field names, duplicate keys rejected, unknown fields rejected,
  no trailing JSON, 1 MiB inclusive limit, valid UTF-8. Decode failures report
  the generic "strict packet decoding failed" detail — no packet contents or
  decoder internals leak into output.
- **Contract validation** (`internal/contract/contract.go`) and the
  outcome -> action table in `specs/packet-v1.md`. Violation ordering and the
  human/JSON rendering are part of the contract.
- **Evidence verification** (`internal/evidence/evidence.go`): resolve the repo
  root, confine evidence paths inside it (reject `..` and symlink escape),
  re-hash the current working-tree file, normalize line endings, compare the
  expected content hash. The packet is input only; `verifier_status` is never
  trusted or rewritten.
- **Anchor re-resolution** (`internal/evidence/relocate.go`): with `--relocate`,
  a failed span is searched for a window of the same line count whose
  fingerprint matches, probing nearest-first from the cited start (lower line
  number first on a tie), with the origin clamped into the range of windows that
  fit and an 8 MiB cap on the total window bytes hashed. It is diagnostic only:
  status, counters, exit code and ledger bytes are unchanged, and a moved span
  stays `failed`. The three clauses - relocates, not found elsewhere, stopped on
  the byte budget - are part of the contract.
- **node-envelope-v1** (`internal/node/`): the same strict-parse rules, a
  canonical packet binding digest, graph validation (duplicate ids and
  references, missing/self dependencies, cycles, size limits), deterministic
  topological evaluation with lexicographic tie-breaking, and an append-only
  JSONL ledger outside the repository with SHA-256 hash chaining
  (`internal/ledger/ledger.go`). The ledger has no timestamps and its exact
  bytes are compared, so it must be reproduced byte-for-byte. The 1 MiB cap is
  a rotation point: a full ledger surfaces as `ledger is full` with the
  archive-aside recovery text, and `ledger verify` audits any file read-only
  (non-regular paths rejected before open).
- **node bind** (`internal/cli/cli.go`): the canonical packet binding digest,
  computed at the shared strict boundary — contract violations exit 1, decode
  failures exit 2 with the generic detail.
- **Path safety**: repository and ledger paths are resolved, symlink ancestors
  are rejected where the Go code rejects them, and inputs are size-bounded.

## Gate (all three must pass)

1. The language's own build and full test suite pass.
2. `python3 spec/parity/harness.py --bin <binary>` reports 28/28.
3. The Go unit tests under `internal/*/*_test.go` are ported for your language
   (at minimum every case they cover), so regressions fail locally.

Then run the shared gates:

```
spec/parity/verify-port.sh <name> <binary> <test command...>
spec/parity/verify-all.sh
python3 spec/parity/fuzz.py --candidate <binary>   # needs a reference build
```

## Divergences

If some recorded behavior cannot be reproduced, write it to
`ports/<lang>/DIVERGENCES.md` with the reason. Never silently approximate.

## Standard library only

OwnScout is deliberately standard-library-only, deterministic, and free of
network, daemon, or repository mutation. Keep that property in the port.
