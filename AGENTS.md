# AGENTS.md

- Purpose: local-first evidence-verification CLI. The Go implementation in `internal/` is the spec; the three ports under `ports/` replay the recorded trace corpus byte-for-byte.
- Definition of done: run `make gate` for reference changes; run `spec/parity/verify-all.sh` for port work; keep the repository `npm`-free.
- Gotcha: any CLI stdout or error-string change requires `make corpus` and committing the regenerated corpus, or corpus-check fails.
- Gotcha: `spec/parity/` is protected; `verify-all.sh` fails if a port lane modifies anything outside `ports/`.
- Gotcha: two packet decoders exist. `internal/cli/adapter.go` `loadPacket` is lenient (case-fold plus last-wins duplicate keys); `internal/nodepacket` is strict. This asymmetry is a pinned contract; see `plans/json-case-fold-divergence.md`. Do not fix it.
- Multiple agents may work in this repository concurrently. Keep commits scoped and leave other agents' in-flight work uncommitted.
