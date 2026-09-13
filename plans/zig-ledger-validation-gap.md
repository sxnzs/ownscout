# Zig port: existing-ledger validation is missing (soundness divergence)

Found by the sibling session (`session-b761e621`), independently verified against
`spec/parity/reference-ownscout`. **Not covered by either corpus and not reached
by `fuzz.py`** — the fuzzer's `ENVELOPE_CASES` always run against a *fresh*
`ledger.jsonl` (`os.remove` before each case), so an existing-ledger validation
bug is invisible to it.

## The bug

`ports/zig/src/cli.zig` `appendLedger` (~line 1368) reads an existing ledger and,
for each line, only checks that `seq` is a `.number` and `record_hash` is a
`.string`, then takes the last line's `seq`/`record_hash` and appends. It performs
**none** of the reference's `internal/ledger/ledger.go` `validateLedger` +
`validateRecord` + `hashRecord` checks on the *existing* records:

- `record_hash` is not recomputed (a forged hash is trusted)
- `prev_record_hash` is not chained (a genesis record with `prev = ffff…` passes)
- `seq` continuity is not enforced (`seq = 99` as the first line passes)
- `schema_version`, field canonicality, `node_results` validity, NUL and size
  limits are all unchecked

A corrupted or hand-forged ledger is **silently trusted and extended**, which is
the append-only integrity property OwnScout exists to provide.

## Verified divergence matrix

`node verify --repo fixtures/repo --packet fixtures/packet-valid.json
--envelope fixtures/envelope-valid.json --ledger <file> --json`, where `<file>`
contains one hand-written record:

| Ledger line | reference | TS | Rust | Zig |
|---|---|---|---|---|
| `record_hash = 0×64` (wrong) | ledger could not be opened | ✓ | ✓ | **all nodes are evidence_current** |
| `seq = 99` (should be 1) | ledger could not be opened | ✓ | ✓ | **all nodes are evidence_current** |
| `prev_record_hash = f×64` (not genesis) | ledger could not be opened | — | — | **all nodes are evidence_current** |
| `schema_version = "WRONG"`, `status = "bogus"` | ledger could not be opened | — | — | **all nodes are evidence_current** |
| case-folded `SCHEMA_VERSION` | ledger could not be opened | ✓ | ✓ | **all nodes are evidence_current** |
| non-JSON garbage | ledger could not be opened | ✓ | ✓ | ✓ |

(`✓` = matches reference; `—` = not re-tested but same code path.)

## Reference behavior to reproduce

`internal/ledger/ledger.go` `Open` → `validateLedger` runs, per line:
`decodeRecord` (DisallowUnknownFields + `consumeObject` with the
`non-canonical JSON field` folded-name check), then `validateRecord`
(schema_version, seq continuity, prev_record_hash chain, sha256 shapes,
node_results identifier/status/NUL/size), then `hashRecord` recomputation and a
`record_hash does not match canonical record` comparison. Any failure →
`validate ledger %q: …` → the CLI renders `ledger could not be opened` /
`ledger open failed`, exit 2.

## Suggested fix scope

In `appendLedger`, replace the per-line `seq`/`record_hash` extraction with a
real validation pass over each existing line: parse strictly (known fields only,
folded-name → non-canonical rejection), recompute the canonical record hash,
verify seq and the prev-hash chain. The byte-for-byte ledger contract means the
recompute must match `hashRecord`'s exact `json.Marshal` output — including
`reason` `omitempty` and field order.

Because this needs a corpus fixture to pin (the fuzzer can't reach it), the fix
should land with edge-corpus cases for each rejected shape above.
