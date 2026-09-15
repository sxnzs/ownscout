# OwnScout and DeltaDB primitives

OwnScout does not copy DeltaDB's implementation. It applies the useful
primitive ideas to a local evidence verifier: make work addressable, preserve
ancestry, keep references tied to content, and make the audit readable without
the checkout that produced it.

This note records which of those ideas are actually implemented, and which are
deliberately not, so the mapping can be checked against the code rather than
assumed.

## Implemented mapping

| Primitive | OwnScout implementation |
|---|---|
| Fine-grained, stable, ordered history | `node verify` appends one deterministic JSONL record. `seq`, `prev_record_hash`, and canonical `record_hash` form an append-only identity chain. |
| Artifact and decision stay bound | Each record stores the envelope digest, canonical packet binding, OwnScout version, and ordered node results. Evidence is re-hashed from the current tree; producer verifier claims are ignored. |
| References are content-addressed | Evidence uses `content_hash` plus a line range. The range is a locator, while the hash is the integrity assertion; mismatches fail closed and are never repaired. |
| Shared history is distinct from a checkout | The ledger is explicitly outside the repository and repository files are read-only. `ledger verify` needs only a ledger file, so a copied audit can be inspected without the original checkout. |
| Offline-first, reproducible operation | No network, daemon, timestamps, or third-party dependencies. The only write is the explicit ledger append. |

## The implemented audit primitive

`ledger.Verify(path)` is the library boundary for a read-only audit. It bounds
input at the same 1 MiB ledger limit as append, requires a regular file, reuses
the same replay rule as `Open`, and returns the length and stable hash of the
trusted prefix together with the first failure as a fixed rule and line, without
trusting or repairing the remainder.

The CLI exposes this as:

```text
ownscout ledger verify --ledger <file> [--json]
```

An intact audit returns exit `0`. A broken chain returns exit `1`. Missing,
unreadable, non-regular or oversized input returns exit `2`. JSON output uses
the same five-field result envelope as every other command. The command never
creates or modifies a file.

The replay is deliberately fail-closed around concurrent appends: a torn tail is
reported as an invalid audit rather than guessed to be a committed record.

Path hardening differs by intent, and the corpus pins all three readers. The
append path and `ledger rotate` refuse a ledger with a symlinked ancestor. The
read-only `ledger verify` follows one, because inspecting a copied audit is the
point; it inspects the final path with `Lstat` and refuses anything that is not a
regular file, so a symlink as the ledger itself is rejected and a FIFO cannot
hang the audit.

## Deliberately deferred

Packet ancestry (`prev_packet_hash`) remains outside the current packet-v1
contract. Adding it would change the wire contract and every independent port;
it should be proposed as a new version rather than smuggled into v1.

A ledger query that reports whether a *sequence* of packet bindings appears in
the trusted prefix is also not implemented. `ledger verify` answers "is this
chain intact" and stops there; a sequence query would add a flag, its own
validation rules, corpus cases and three port implementations, so it belongs in
its own change rather than in the audit primitive's introduction.
