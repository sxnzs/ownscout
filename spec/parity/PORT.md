# OwnScout port contract

This directory records the exact behavior a port must reproduce. The Go
implementation under `internal/` is the reference; `corpus.json` is the
oracle. Read the Go source — it is the specification.

## What to build

A native implementation of the OwnScout CLI under `ports/<lang>/`, producing a
binary with the same commands, the same stdout, and the same exit codes.

Commands:

- `ownscout doctor`
- `ownscout version` -> `ownscout 0.1.0`
- `ownscout contract validate --packet <file> [--json]`
- `ownscout evidence verify --repo <dir> --packet <file> [--json]`
- `ownscout node verify --repo <dir> --packet <file> --envelope <file> --ledger <file> [--json]`
- `--help` at the root and for each command, plus `ownscout <cmd> --help`

## Exit codes

- `0` success, valid packet, or verified evidence
- `1` contract failure, evidence failure, or node graph failure after append
- `2` usage error, malformed input, missing file, or repository/ledger I/O

## The oracle

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

`corpus-edge.json` (59 cases) is a hardening set: every contract-testdata
fixture, the subcommand surface, synthesized node-envelope graph failures, raw
JSON parse failures, a graph whose evidence verification fails (exit 1, with a
ledger), and an in-repository ledger rejection. Run it with the same harness:

```
python3 spec/parity/harness.py --corpus spec/parity/corpus-edge.json --bin <binary>
```

## Oracle validation

The corpus was mutation-tested against two deliberately broken reference builds:

| Mutation | Base corpus | Edge corpus |
|---|---|---|
| `SetEscapeHTML(false)` in the JSON result encoder | 26/28 | 73/81 |
| `next_action` renamed to `nextAction` | 20/28 | 45/81 |
| unmutated reference | 28/28 | 81/81 |

Both subtle divergences are caught, so a passing harness is meaningful rather
than vacuous.

## Behavior to preserve (read the Go source)

- **packet-v1 strict decoding** (`internal/nodepacket/decode.go`): exactly one
  JSON object, exact case-sensitive field names, duplicate keys rejected,
  unknown fields rejected, no trailing JSON, 1 MiB inclusive limit, valid UTF-8.
- **Contract validation** (`internal/contract/contract.go`) and the
  outcome -> action table in `specs/packet-v1.md`. Violation ordering and the
  human/JSON rendering are part of the oracle.
- **Evidence verification** (`internal/evidence/evidence.go`): resolve the repo
  root, confine evidence paths inside it (reject `..` and symlink escape),
  re-hash the current working-tree file, normalize line endings, compare the
  expected content hash. The packet is input only; `verifier_status` is never
  trusted or rewritten.
- **node-envelope-v1** (`internal/node/`): the same strict-parse rules, a
  canonical packet binding digest, graph validation (duplicate ids and
  references, missing/self dependencies, cycles, size limits), deterministic
  topological evaluation with lexicographic tie-breaking, and an append-only
  JSONL ledger outside the repository with SHA-256 hash chaining
  (`internal/ledger/ledger.go`). The ledger has no timestamps and its exact
  bytes are compared, so it must be reproduced byte-for-byte.
- **Path safety**: repository and ledger paths are resolved, symlink ancestors
  are rejected where the Go code rejects them, and inputs are size-bounded.

## Gate (all three must pass)

1. The language's own build and full test suite pass.
2. `python3 spec/parity/harness.py --bin <binary>` reports 28/28.
3. The Go unit tests under `internal/*/*_test.go` are ported for your language
   (at minimum every case they cover), so regressions fail locally.

## Divergences

If some recorded behavior cannot be reproduced, write it to
`ports/<lang>/DIVERGENCES.md` with the reason. Never silently approximate.

## Standard library only

OwnScout is deliberately standard-library-only, deterministic, and free of
network, daemon, or repository mutation. Keep that property in the port.
