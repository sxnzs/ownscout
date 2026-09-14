# OwnScout node-envelope v1

A node envelope declares the verification graph `node verify` evaluates. It is
read through the same strict decode boundary as the packet: unknown fields,
duplicate keys, non-canonical numbers, and inputs over 1 MiB all fail as
`envelope could not be parsed`.

Required envelope fields:

- `envelope_id`
- `packet_id`
- `packet_binding_sha256`
- `schema_version`
- `nodes`

Required node fields:

- `node_id`
- `verifier`
- `depends_on`
- `evidence_ids`

Field rules:

- `schema_version` must equal `node-envelope-v1`.
- `packet_id` must equal the packet's `packet_id`.
- `packet_binding_sha256` must equal the recomputed canonical packet binding
  (lowercase hex) — the same digest `node bind` prints.
- Identifiers (`envelope_id`, `packet_id`, `node_id`, every `depends_on` and
  `evidence_ids` entry, and every packet `evidence_id`) are 1-128 characters;
  the first must be ASCII alphanumeric and the rest alphanumeric or
  `.` `_` `-` `:`.
- `nodes` is a non-empty array of at most 256 entries; `node_id` is unique
  across the envelope.
- `verifier` must equal `evidence.current`.
- `depends_on` is a non-null array of at most 128 unique identifiers; every
  entry must resolve to a declared `node_id`, self-dependencies are rejected,
  and the graph must be acyclic.
- `evidence_ids` is a non-empty, non-null array of at most 128 unique
  identifiers; every entry must resolve to an `evidence_id` the packet
  declares.
- All `depends_on` and `evidence_ids` entries across the envelope total at
  most 4096 references.

Evaluation is deterministic Kahn topological order: the lexicographically
smallest ready node runs first, including nodes that become ready mid-order.
Each node ends in exactly one status:

| Status | Meaning |
|---|---|
| `evidence_current` | every `depends_on` is `evidence_current` and every `evidence_ids` entry verified |
| `failed` | dependencies held but at least one cited evidence span did not verify |
| `blocked` | at least one `depends_on` is not `evidence_current` (`reason` names the first such dependency) |

Every `node verify` appends one record per run to an append-only JSONL ledger
that must live outside the repository; the record's `node_results` array
carries one entry per node in evaluation order. Records are SHA-256 chained (`seq`,
`prev_record_hash`, `record_hash`) with no timestamps, so the exact bytes are
the contract. The ledger is capped at 1 MiB: a full ledger surfaces as
`ledger is full` and is a rotation point — archive it aside and the next
verification starts a fresh chain; `ledger verify` audits any file read-only
and `ledger rotate` verifies-then-renames it to `<file>.<tip8>`.
