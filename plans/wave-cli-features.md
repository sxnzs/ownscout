# Wave plan: CLI feature batch via sub-agents

> **Complete 2026-09-13.** Waves 1 and 2 both landed. Two constraints below are
> now historical, kept for the record:
>
> - The "two packet decoders" asymmetry was **unified** in `51b140e`: every
>   command now decodes through `internal/nodepacket.DecodeValid` (strict).
>   The former lenient `encoding/json` path is retired; the corpus and ports
>   were regenerated and re-verified against the single strict boundary.
> - The "`ports/` is an external workstream" exclusion is no longer in force.

Executed 2026-09-13 by a pi-deck crew (`runCrew`, model `openai-codex/gpt-5.6-luna`),
orchestrated from a live pi session. Disjoint write scopes; generated corpora are
single-writer (orchestrator).

## Constraints in force at the time

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

## Wave 2: port parity (running after the case-fold lane committed)

Three crew tasks, one per port, disjoint scopes (`ports/ts`, `ports/rust`,
`ports/zig`). Each implements `node bind`, `ledger verify`, and
`node verify --relocate` against the recorded cases (18 new edge cases at
190 total) plus the updated usage strings in the base corpus.

| # | Node | Owner | Write scope | Validation |
|---|---|---|---|---|
| W2.1 | ts port | crew `port-ts` | `ports/ts/` | `verify-port.sh ts ports/ts/bin/ownscout 'cd ports/ts && node --test'` |
| W2.2 | rust port | crew `port-rust` | `ports/rust/` | `verify-port.sh rust ports/rust/target/release/ownscout 'cd ports/rust && cargo test --quiet'` |
| W2.3 | zig port | crew `port-zig` | `ports/zig/` | `verify-port.sh zig ports/zig/zig-out/bin/ownscout 'cd ports/zig && zig build test'` |
| W2.4 | integration | orchestrator | `docs/`, `README.md` | `spec/parity/verify-all.sh` green |

## Deferred (recorded, not dropped)

- ~~**Decoder unification** (`loadPacket` → `nodepacket.Decode`)~~ — **done**
  in `51b140e`. All commands share the single strict boundary; the corpus and
  the three ports were updated and verified green (`28/28` + `190/190`).
- ~~**Ledger 1 MiB rotation policy**~~ — **done**. The cap stays at 1 MiB and a
  full ledger is a rotation point: `mv` it aside and the next append starts a
  fresh chain. `ledger verify` audits each file independently; `node verify`
  surfaces it as `ledger is full` via `ledger.FullError`. Documented in
  `AGENTS.md`; corpus case `ledger-full.jsonl` pins the boundary.
