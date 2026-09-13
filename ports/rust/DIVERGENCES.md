# Rust port divergences

Divergences from the Go reference that this port does not currently reproduce.
Neither is covered by `corpus.json` or `corpus-edge.json`, so both recorded
corpora pass; they are reachable only through the fuzzer:

```
python3 spec/parity/fuzz.py --candidate ports/rust/target/release/ownscout --iterations 300 --seed 13
```

Both were present before the anchor re-resolution work and are untouched by it -
`ports/rust/src/packet.rs` is unmodified - and both remain open. They are
recorded here rather than silently approximated, per `spec/parity/PORT.md`.

## 1. Unknown-field detection is not in document order

The reference reports whichever decode error occurs **first in the input**. This
port detects unknown fields in a pass of its own, so it reports an unknown field
even when an earlier field has the wrong type.

Given a packet whose `budget.used_evidence` is a string and which later contains
an unknown `budget.max_Dbytes`:

```
Go:   packet "..." is not valid JSON: json: cannot unmarshal string into Go struct field Packet.budget.used_evidence of type int
Rust: packet "..." contains an unknown JSON field: json: unknown field "max_Dbytes"
```

Order is observable in both directions: an unknown field placed before the
badly-typed one is reported by both implementations, so only the
bad-field-then-unknown-field case diverges.

## 2. Field names are escaped Rust-style, not Go-style

The reference quotes a field name the way Go's `%q` does, escaping a
non-printable byte as `\xNN`. This port formats the name with Rust's `{:?}`,
which emits `\u{NN}`.

For an unknown field named `evidence\x7fid` (DEL, one byte):

```
Go:   json: unknown field "evidence\x7fid"
Rust: json: unknown field "evidence\u{7f}id"
```

`spec/parity/fuzz.py --seed 13` reaches both shapes through byte mutation of
`fixtures/packet-valid.json`; seed 7 does not.
