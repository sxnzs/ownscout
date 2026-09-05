# OwnScout packet contract v1

A packet is the only artifact Astra may consume.

Required packet fields:

- `packet_id`
- `schema_version`
- `repo_root`
- `head_commit`
- `request_id`
- `issued_at`
- `outcome`
- `freshness`
- `authorization`
- `budget`
- `evidence`
- `degradations`
- `provenance`
- `packet_hash`

Required evidence fields:

- `evidence_id`
- `kind`
- `path`
- `commit`
- `line_start`
- `line_end`
- `source`
- `content_hash`
- `collected_at`
- `verifier_status`

Outcomes map to one default action:

| Outcome | Action |
|---|---|
| `complete` | `autonomous_proceed` |
| `partial` | `bounded_more_evidence` |
| `partial_degraded` | `autonomous_proceed` |
| `no_match` | `autonomous_proceed` |
| `stale` | `bounded_refresh` |
| `unavailable` | `blocked` |
| `blocked` | `blocked` |
| `needs_more_evidence` | `bounded_more_evidence` |
| `failed_verification` | `quarantine` |
| `budget_exhausted` | `human_approval` |

`complete` requires verified evidence, current freshness, no degradations, and
budget counters within limits.
