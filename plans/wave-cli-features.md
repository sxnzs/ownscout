# Wave plan: CLI feature batch via sub-agents

Executed 2026-09-13 by a pi-deck crew (`runCrew`, model `openai-codex/gpt-5.6-luna`),
orchestrated from a live pi session. Disjoint write scopes; generated corpora are
single-writer (orchestrator).

## Constraints in force

- `ports/` is an active external workstream (case-fold implementation in flight);
  nothing in this wave may touch it.
- The two packet decoders (`cli/adapter.go` lenient vs `nodepacket` strict) are
  pinned contract — `plans/json-case-fold-divergence.md` + corpus cases freeze the
  asymmetry. Decoder unification is therefore a spec decision, deferred.
- `spec/parity/` corpora are generated artifacts: regenerated only by the
  orchestrator at integration, never by a task agent.

## Nodes

| # | Node | Owner | Write scope | Validation |
|---|---|---|---|---|
| N1 | Repo `AGENTS.md` + `make gate` in both CI pipelines | crew `docs-ci` | `AGENTS.md`, `.github/workflows/ci.yml`, `.gitlab-ci.yml` | `go build ./...` |
| N2 | `node bind`, `ledger verify`, `node verify --relocate` | crew `features` | `internal/`, `cmd/` | `go vet ./... && go test ./...` |
| N3 | Corpus cases for the new surface + regenerate | orchestrator | `tools/`, `spec/parity/` | `make gate` |
| N4 | Independent diff review | crew `reviewer` | none (read-only) | verdict + issues |
| N5 | Commit, README usage lines, report | orchestrator | `README.md` | gate green |

## Deferred (recorded, not dropped)

- **Decoder unification** (`loadPacket` → `nodepacket.Decode`): currently
  contradicts the pinned two-decoder contract. Requires a spec decision: keep
  the documented asymmetry, or version the contract. Not a code question.
- **Port parity for the new commands**: the next wave, after the case-fold lane
  commits. Adding corpus cases for `node bind`/`ledger verify` will read red on
  `status.sh` until then — honest intermediate state.
- **Ledger 1 MiB rotation policy**: a product decision (cap, archive, or
  documented rotation), deferred with it.
