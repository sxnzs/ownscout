# OwnScout node-envelope-v1 prewalk plan

## Goal

Add a strict local evidence graph checker. It proves only that a node's cited
evidence is current at invocation time. It does not prove semantic correctness,
task completion, or edit acceptance.

## Non-negotiable decisions

- Keep packet-v1 and its current CLI contract unchanged.
- Add a separate strict `node-envelope-v1` sidecar.
- Envelope binds to `packet_id` and an OwnScout-computed canonical packet digest.
  The producer-asserted `packet_hash` is never treated as a binding.
- The only allowed verifier ID is `evidence.current`.
- Node statuses are exactly `evidence_current`, `failed`, and `blocked`.
- Empty evidence references are invalid; no node can pass vacuously.
- Validate graph structure before checking evidence: duplicate IDs/references,
  null arrays, unknown fields/keys, missing dependencies, self-dependencies,
  cycles, malformed identifiers, and excessive size all fail.
- Evaluate in deterministic topological order with lexicographic node-ID
  tie-breaking.
- Any dependency not `evidence_current` blocks dependents. Independent branches
  continue.
- Ledger is explicit append-only JSONL outside the repository. It is audit
  history, never authorization. It is opened and appended safely, never repaired.
- CLI output keeps the stable five fields: `command`, `ok`, `summary`,
  `details`, `next_action`.
- Exit semantics: invalid input/envelope/ledger path = 2; graph failure = 1;
  successful ledger append with all nodes evidence_current = 0; failed ledger
  append = 2 even if evidence checks passed.

## Prewalk ownership

- Astra owns the first hard node: `internal/node` schema, canonical packet
  binding, strict graph validation, deterministic evaluation, and focused tests.
- Fable owns the second hard node: `internal/ledger` append-only record format,
  path/alias safety, hash chaining, locking, and focused tests.
- A cheap Flash lane will later implement CLI integration and docs from the
  proven package APIs.

## Explicitly rejected for v0

Shell commands, model routing, dynamic verifier plugins, daemon, network,
repository mutation, semantic completion claims, signatures, leases, and a
message bus.
