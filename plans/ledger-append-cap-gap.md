# Append-time ledger cap is unpinned (coverage gap)

Found 2026-09-13 while auditing the ledger-rotation wave (`1ad8ccd` +
`6547bf2`). Verified live against a freshly built reference binary. **Not
covered by either corpus** — this is the same gap *shape* as the Zig
`appendLedger` soundness bug: the corpora exercise the 1 MiB cap only at
**open** time, never at **append** time.

## The gap

`internal/ledger/ledger.go` has two distinct size checks:

| Path | Site | Trigger |
|---|---|---|
| `Open` | `ledger.go:132,143` | existing file `> 1 MiB` |
| `Append` | `ledger.go:223-224` | `len(encoded) > maxLedgerSize - info.Size()` |

`cli.go` wraps **both** `Open` and `Append` errors in `errors.As(&full)` →
`ledger is full`. But the only full-ledger corpus fixture is
`spec/parity/fixtures/edge/ledger-full.jsonl` at **1,048,593 bytes** — already
over the cap *before* the command runs, so `Open` rejects it and `Append` never
executes. The `node-edge-ledger-append-*` cases use `ledger-seed.jsonl`
(483 bytes, one record) — a *successful* append.

Result: the `Append` `FullError` path is dead code as far as the corpus is
concerned. A port that skips the append-time cap stays green.

## Proof (reproduced)

Built a valid, fully-chained ledger just under the cap: **2159 records,
1,048,167 bytes**. The next record (~483 bytes) overflows.

```
ledger verify  -> ok:true   "ledger is intact"  (2159 record(s))     # Open accepts
node verify    -> ok:false  "ledger is full"    ledger exceeds 1048576 bytes   # Append FullError
```

The reference emits `ledger is full` on the append path — confirmed working.
What is missing is the *corpus case* that pins it.

## Why it matters for ports

`6547bf2` added the append cap to all three ports (Zig: `cli.zig`
`output.items.len > ledger_max_size → LedgerFull`; the error is surfaced
correctly). But with no corpus case, that line is unverified — a port that
dropped it would not be caught. Zig's `DIVERGENCES.md` previously listed this
exact behavior as *unimplemented*; it is now implemented but still unpinned.

## Proposed case

Add a `ledger-nearly-full.jsonl` fixture (valid chained records summing to
`1 MiB − ~one record`) plus a `node verify` case pair asserting
`ledger is full` / `ledger exceeds 1048576 bytes`. Because the fixture must be
a *valid* chain (forged lines are rejected before the size check), it is easiest
generated the way `ledger-seed.jsonl` already is — by running the reference to
append records until just under the cap — rather than hand-written.

`tools/gen-corpus-edge/main.go` already regenerates `ledger-seed.jsonl` by
running the binary (the `seedArgs`/`seedRun` block); a `ledger-nearly-full`
fixture can reuse that loop with a stop-at-`FullError` condition.

## Note on the committed reference binary

`spec/parity/reference-ownscout` (built 14:19) predates `1ad8ccd` (14:33). On
the near-full ledger it **does** refuse the append (exit 2, nothing written —
the `Append` `FullError` fires) but reports the *pre-unification* wording
`ledger append failed` / `node results could not be appended`, not `ledger is
full`. The open-time cap is already correct on it (`ledger-full.jsonl` →
`ledger exceeds 1048576 bytes`).

This staleness is narrower than it first looks: `make corpus` / `make
fuzz-sweep` both rebuild `$(REF)` from source first (`reference: go build -o
$(REF)`), so the committed file self-heals on any gated path. It only bites a
**bare** `python3 spec/parity/fuzz.py` or `harness.py --bin` call that skips the
rebuild — which is exactly how a port lane verifying by hand could read stale
"ledger append failed" as the oracle. Rebuilding the committed binary (or not
committing a binary at all) removes the trap.

## Resolved

Pinned in `73cca46`, in the round that acted on this note. `make corpus` now
builds `fixtures/edge/ledger-near-full.jsonl`: a valid, fully chained ledger of
1,048,374 bytes, so `Open` accepts it and the record `node verify` appends
(about 483 bytes) cannot fit. Four cases record the split — `ledger verify`
accepts the file, `node verify` refuses the append with `ledger is full` — and
the generator asserts the refusal before recording anything, because a fixture
one byte too small would let the append succeed and rewrite itself under the
remaining cases.

The gap was not theoretical. Every port that skipped the append-time check failed
the new cases: Rust was writing past 1 MiB and reporting success until `52f94ec`
enforced the cap, and the fixture now keeps that from regressing.

Two things this note got right that are worth keeping:

- The size check has two distinct sites and the corpus has to reach both. A
  fixture that is *already* over the cap only ever exercises `Open`.
- The committed-path concern was real, and `fuzz.py` now warns when the reference
  binary is older than the Go sources (`48fbdc9`), so a lane cannot read a stale
  oracle as the truth.
