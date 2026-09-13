# AGENTS.md

- Purpose: local-first evidence-verification CLI. The Go implementation in `internal/` is the spec; the three ports under `ports/` replay the recorded trace corpus byte-for-byte.
- Definition of done: run `make gate` for reference changes; run `spec/parity/verify-all.sh` for port work; keep the repository `npm`-free.
- Gotcha: any CLI stdout or error-string change requires `make corpus` and committing the regenerated corpus, or corpus-check fails.
- Gotcha: `spec/parity/` is protected; `verify-all.sh` fails if a port lane modifies anything outside `ports/`.
- Gotcha: every command decodes packets through `internal/nodepacket` (strict: exact field names, duplicate keys rejected, 1 MiB cap). The former lenient `encoding/json` path on contract/evidence was removed by the decoder unification; `plans/json-case-fold-divergence.md` records the retired asymmetry for history.
- Multiple agents may work in this repository concurrently. Keep commits scoped and leave other agents' in-flight work uncommitted.
