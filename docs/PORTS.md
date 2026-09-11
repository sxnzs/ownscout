# OwnScout language ports

Three independent, standard-library-only ports of the Go reference in `internal/`,
all verified byte-for-byte against the same recorded oracle.

## Status (2026-09-11)

| Port | Binary | Base corpus | Edge corpus | Tests | Third-party deps |
|---|---|---|---|---|---|
| TypeScript | `ports/ts/bin/ownscout` | 28/28 | 93/93 | 106 | none (Node built-ins only) |
| Zig | `ports/zig/zig-out/bin/ownscout` | 28/28 | 93/93 | 16 | none (`.dependencies = .{}`) |
| Rust | `ports/rust/target/release/ownscout` | 28/28 | 93/93 | 17 | none (empty `[dependencies]`) |

The Go reference itself is unchanged: 62 tests, `node` 98.7% / `nodepacket` 94.5%
coverage, `go test -race` clean.

## How to verify

```
make gate                    # Go reference tests + corpus freshness
spec/parity/verify-all.sh    # both corpora + each port's tests + protected paths
spec/parity/status.sh        # compact per-port status, never fails
```

Differential fuzzing (reference vs candidate on mutated fixtures):

```
make reference
python3 spec/parity/fuzz.py --candidate <binary> --iterations 300
```

## Verification evidence

- Base corpus: 28 recorded CLI cases (human and `--json`, exit codes 0/1/2, ledger).
- Edge corpus: 93 cases (path escape, non-UTF8, graph cycles, ledger hash
  chaining, in-repo ledger rejection, Go JSON HTML-escaping, and null/empty
  field shapes).
- Oracle mutation test: two deliberately broken reference builds fail the
  corpora (26/28 and 20/28 base; 85/93 and 51/93 edge), so a pass is meaningful.
- Fuzzing: no divergence in 200 iterations (seed 1) plus 300 iterations
  (seed 7) per port, and seed 11 found the null-handling defect recorded below.

## Bugs the fuzzer caught

### Unknown-field reporting (seed 1)

All three ports initially passed both static corpora, but the fuzzer found the
same real defect in each: JSON syntax errors were reported as
`contains unknown JSON field`, and the `json: unknown field "<name>"` detail
was dropped. The reference runs `json.Valid` first, then decodes with
`DisallowUnknownFields`, then checks for trailing data. Ports must reproduce
that order and those exact strings. Fixed in all three.

### Null versus empty fields (seed 11)

A later run with a fresh seed found a second shared defect. The reference keys
required-field presence off *emptiness* for structs (`freshness`, `budget`) and
*nil-ness* for slices (`evidence`, `degradations`): an explicit JSON `null` is a
missing required field, while `[]` is present but still triggers the
"complete packet requires at least one evidence entry" rule. TypeScript and
Rust skipped the evidence rule when the field was absent or null; Zig used
key-presence for structs and treated null slices as present, even reporting
`"degradations": null` as valid.

Fixed in all three, and the shapes are now frozen into the edge corpus
(`internal/contract/testdata/invalid-null-*.json` and
`invalid-empty-*.json`), so the static gate catches them without the fuzzer.

## Port design

- **TypeScript** — a single `ownscout.ts` run directly by Node 24 type stripping,
  no build step and no `package.json`; `ports/ts/bin/ownscout` is a shell wrapper.
- **Rust** — `src/` modules with a hand-rolled JSON reader/writer and an in-tree
  SHA-256, because the crate must stay dependency-free.
- **Zig** — explicit-allocator modules under `src/`; token-based JSON reader.

## How they were built (not derivable from git)

- Built by `droid exec` lanes against `spec/parity/PORT.md`.
- **Model choice was decisive.** `gemini-3.8-flash` and `glm-5.3-flash` spent 20
  minutes analysing and produced a 9-line stub and nothing; `gpt-5.6-luna`
  completed a full port in 5-13 minutes. All lanes were relaunched on luna.
- A write-first, stage-gated brief (max 10 minutes of reading, then build and run
  the harness after each of six stages) is what made the lanes productive; an
  earlier open-ended brief caused exhaustive re-reading and context blowups.
- droid sessions for follow-ups (`droid exec -s <id>`): TypeScript
  `a5330512-8176-4335-9350-d28688bb3720`, Zig
  `7cbb340e-8fae-4c5c-8e50-b934b51e2ec8`, Rust
  `9b93b98f-67ee-4c10-a062-3fbc9e0cb03b`. `spec/parity/status.sh` keeps this map.
- An earlier attempt was lost when the harness restarted and killed the
  background lanes before they had written anything; the staged brief now leaves
  durable partial work.

## Commits

Parity layer: `caee7f2` corpus, `0b9d247` edge corpus, `ced66f3` verifier,
`1c5dcc5` ledger chaining, `0dcb0e9` oracle mutation test, `497e91b` fuzzer,
`2c8e247` all-ports grader, `27d81ff` Makefile, `b61f237` layout.
Ports: `59d701b` TypeScript, `e5c3c9d` Rust, `9c62f3b` Zig, `aeb2357`/ `c7b2f3a`
tests, `9c2f069` fuzz fix.
