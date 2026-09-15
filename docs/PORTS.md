# OwnScout language ports

Three independent, standard-library-only ports of the Go reference in `internal/`,
all verified byte-for-byte against the same recorded trace corpus.

## Status (2026-09-13)

| Port | Binary | Base traces | Edge traces | Tests | Third-party deps |
|---|---|---|---|---|---|
| TypeScript | `ports/ts/bin/ownscout` | 35/35 | 249/249 | 149 | none (Node built-ins only) |
| Zig | `ports/zig/zig-out/bin/ownscout` | 35/35 | 249/249 | 37 | none (`.dependencies = .{}`) |
| Rust | `ports/rust/target/release/ownscout` | 35/35 | 249/249 | 38 | none (empty `[dependencies]`) |

The Go reference's observable behaviour is unchanged: 78 tests,
`node` 98.7% / `nodepacket` 94.5% coverage, `go test -race` clean. Evidence
verification hashes cited ranges in place (no intermediate copies), with a
differential reference kept in the test package, and `evidence verify --relocate`
re-resolves a failed span against its recorded content fingerprint.

## How to verify

```
make gate                    # Go reference tests + corpus freshness
spec/parity/verify-all.sh    # both trace corpora + each port's tests + protected paths
spec/parity/status.sh        # compact per-port status, never fails
```

Differential fuzzing (reference vs candidate on mutated fixtures):

```
make reference
python3 spec/parity/fuzz.py --candidate <binary> --iterations 300
```

## Verification evidence

- Base trace corpus: 35 recorded CLI cases (human and `--json`, exit codes 0/1/2, ledger).
- Edge trace corpus: 249 cases (path escape, non-UTF8, graph cycles, ledger hash
  chaining, in-repo ledger rejection, Go JSON HTML-escaping, null/empty
  field shapes, the raw-byte evidence shapes: invalid UTF-8 and CRLF in
  evidence files, a single empty selected line, oversize evidence files, and
  range errors that must report the real line count, the anchor re-resolution
  outcomes: moved, absent, shrunken file, and budget-stopped, the
  unknown-field surface: Go `%q` quoting of a field name across `\x`, `\u` and
  `\U` widths and which of two decode errors in one nested object is reported,
  and the ledger JSON-walk structural rejections: duplicate keys flat and
  nested, unknown and case-folded fields, non-object records, trailing data,
  a missing final LF, blank lines, and node_results shape and per-result
  rules).
- Trace corpus mutation test: five deliberately broken reference builds fail the
  trace corpora (base 33/35, 25/35, 35/35, 35/35 and 33/35; edge 206/249, 124/249,
  247/249, 247/249 and 249/249 — fractions as recorded against the 35+249 corpus those builds
  faced), so a pass is meaningful. The quoting and precedence cases cannot be
  mutation-tested this way, because the reference's behaviour there comes from
  the decoder; they are validated instead by all three ports failing them
  before the fix (TypeScript 6, Rust 8, Zig 8 of the new cases).
- Fuzzing: no divergence in 300 iterations at seeds 7 and 13, and 200 at seed 1,
  per port. Seeds 1, 11 and 13 each found a defect that is now fixed and frozen
  into the edge trace corpus; see below. No open divergences are known.

## Bugs the fuzzer caught

### Unknown-field reporting (seed 1)

All three ports initially passed both static trace corpora, but the fuzzer found the
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

Fixed in all three, and the shapes are now frozen into the edge trace corpus
(`internal/contract/testdata/invalid-null-*.json` and
`invalid-empty-*.json`), so the static gate catches them without the fuzzer.

### Node and contract usage errors (the relocation round)

Adding the `contract-edge-relocate-unknown-flag-json` case exposed a Rust defect
both earlier trace corpora had missed: contract and evidence usage errors were
rendered as human text even under `--json`. With that fixed, a direct
candidate-vs-reference comparison found two more Rust-only divergences that no
case covered - `node verify` printed human text under `--json`, and it reported
`--ledger` as the first missing path where the reference reports `--repo`
first. All three are fixed in Rust and frozen into the edge trace corpus
(`node-edge-missing-flags-human`, `node-edge-missing-flags-json`,
`node-edge-missing-flags-partial-json`, `node-edge-unknown-flag-json`), which
also proved that TypeScript and Zig already matched.

### Field-name quoting and decode precedence (seed 13)

The last two divergences the fuzzer could reach, both in packet decoding and both
present in all three ports.

The reference quotes an unknown field name with Go's `%q`, and it reports
whichever decode error occurs first in document order, recursing into nested
objects. Each port got a different half of this wrong: TypeScript escaped `"`,
`\`, tab and newline but emitted DEL, NBSP and U+2028 as raw bytes; Rust used
Rust's `{:?}`, giving `\u{7f}`; Zig emitted control bytes raw, which broke the
single-line result format outright. On precedence, Rust always reported the
unknown field because it scanned for them in a pass of its own, while Zig always
reported the type error because it type-checked first.

The fix needs three escape widths - `\xNN` for control bytes and DEL, `\uNNNN` up
to U+FFFF, `\UNNNNNNNN` above it - with printable runes left literal, and the
unknown-field check interleaved with the type checks so document order decides.
Twelve cases pin it (`contract-edge-unknown-*` and
`contract-edge-precedence-*`). All three ports pass 249/249 and fuzz seed 13 is
clean; the `DIVERGENCES.md` files they had been recorded in are gone.

### Strict decoding on every command (seeds 3-21, then unification)

Fixing the two above moved the fuzzer on to three decoding divergences the
reference then inherited from `encoding/json`'s defaults on the contract and
evidence paths. The decoder unification later removed the lenient path
entirely: every command now decodes through `nodepacket`, and all of these
shapes are hard decode failures everywhere:

- **Case-folded field names** — `SCHEMA_VERSION` no longer maps to
  `schema_version` on any command.
- **Invalid UTF-8 inside field names** — rejected outright rather than
  becoming per-byte U+FFFD substitutions.
- **Null elements in the evidence array** — rejected at decode rather than
  decoding to a zero-valued struct.

The fixtures pin the rejections (`contract-edge-case-top`, `-case-nested`,
`-null-evidence-element`, `-unknown-badutf8`, `-surrogate-escape`). They were
found by sweeping seeds, not by the single seed 13 that the earlier rounds
used - the fuzzer's coverage is seed-dependent, so a clean seed 13 is not
evidence that a port is exact.

### Node-envelope identifier validation (seed 24)

The next class was not about case at all. Identifiers are validated as
1-128 bytes, alphanumeric with `.`, `_`, `-` and `:` allowed only after the first
character, and while the trace corpus covered a null `node_id` it had no character,
length or leading-character case. Seed 24 reached the gap with an `envelope_id`
of `env%lope-1`.

TypeScript already validated every identifier; Rust and Zig validated `node_id`
but not `envelope_id`, so both accepted an envelope the reference rejects. Eight
cases pin the rule (`node-edge-identifier-*`).

### What this says about the gate

Seven divergence classes in a row were reachable only at seeds some earlier run
did not happen to pick. A single seed is not a verification strategy, so the
sweep is now a first-class target and a CI job rather than an ad-hoc command:

```
make fuzz-sweep                 # seeds 3 6 8 13 20 21 24, every port
make fuzz-sweep FUZZ_SEEDS="1 2 3 4 5" FUZZ_ITERATIONS=300
```

`FUZZ_SEEDS`, `FUZZ_ITERATIONS` and `PORT_BINARIES` are overridable, and the
target exits non-zero on the first divergence. Treat "clean at seed N" as
evidence about seed N only.

Sweeping was necessary but not sufficient. The forged-ledger soundness gap was
invisible to *every* seed, because the envelope cases removed `ledger.jsonl`
before each run and so never validated an existing ledger. The fuzzer now seeds
a mutated ledger and runs `ledger verify` and `node verify` against it. The
lesson generalises: when a class of input is never fed to a command, more
iterations of the same seed do not help - the coverage has to change, not the
volume.

### Coverage: which reference code no case reaches

`make coverage` builds the reference with `-cover`, replays both corpora against
it, and prints per-package coverage plus the functions that never executed. It is
the cheapest way to find the next gap, because a path no case reaches is a path a
port can skip and still pass - which is how both the append-time ledger cap and
the symlinked-ancestor check stayed invisible while every port was green.

It now fails when a never-executed function is not on the justified exemption list
in the Makefile, so an entry cannot sit there unnoticed the way two did. An
exemption is a claim that no corpus case *can* reach the code, not that writing one
would be inconvenient; each carries its reason beside the list. The test for
whether a new entry belongs there is whether a porter could get it wrong - a Go
error-chain method cannot be ported wrongly, a ledger path check can.

Current reading: `cmd` 100%, `cli` 92.1%, `contract` 89.3%, `evidence` 87.1%,
`ledger` 77.1%, `node` 83.8%, `nodepacket` 83.5%, with two exempt functions: the
`evidence.VerifyPacket` wrapper, which only the unit tests and benchmarks call
because no corpus case can invoke a Go API, and `ValidationError.Unwrap`, which is
reached only if something wraps that error and nothing does. `ledger` is still the
low package - the ledger shape cases lifted it from 71.1% and covered
`ledger.isPathPrefix` - and it is where every gap of this shape has been found, so
it is the first place to look next.

### Keeping the counts honest

Case counts are restated in many places - two badges, several prose
sentences, a status table, the port contract, the port guide and two diagrams -
and every one is a hand-maintained copy. They drift silently: a diagram once read
`93 EDGE` beside a card reading `208`, and the port contract's mutation table
carried `/190` denominators under a `208/208` total. No gate caught either,
because the corpora themselves were consistent.

`make docs-check` derives the counts from `spec/parity/corpus.json` and
`corpus-edge.json` and asserts every restatement agrees, naming the file, line
and value on failure. It is part of `make gate` and runs in the reference job of
both pipelines.

What it checks: both case counts everywhere they appear, including every
occurrence in a file rather than the first, so one document contradicting itself
fails; the denominator of every mutation-table row against the corpus it was
measured on; the spelled-out mutation count ("five deliberately broken reference
builds" in five files) against the number of mutation rows in the port
contract; and, for a quantity the corpus cannot supply, the README test total
against the sum of the per-language counts in the parity diagram.

What it does not check: mutation **numerators**, per-language numerators and
the coverage percentages above. All three require running things - a broken
build per mutation, four test suites, and an instrumented replay of both corpora
- rather than reading the corpus.

Two of the three are now measured by a command rather than by hand:

- `make mutants` re-measures the mutation table. It stages a copy of the module
  per mutation, applies one exact edit, builds it and replays both corpora, then
  rewrites the governed region of `spec/parity/PORT.md`. It never touches the
  working tree. If an anchor no longer matches, it fails loudly instead of
  silently reporting a mutation the corpus "catches".
- `make mutants-check` re-measures and fails if the committed table disagrees.
  It runs in the reference job of both pipelines, not in `make gate`: a warm gate
  is under 10s and this costs about 33s, which is too much to put in front of
  every local change.

The per-language test counts are not hand-maintained either: they are bound to
the parity diagram, which is bound to the README badge, and the diagram's Go
count is compared against `go test -list` so it cannot age the way it did when a
test was added and the diagram kept the old figure.

When the corpus grows, run `make mutants` and `make coverage` and commit the
result; `docs-check` will insist the mutation denominators match in the
meantime.

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

Parity layer: `caee7f2` trace corpus, `0b9d247` edge trace corpus, `ced66f3` verifier,
`1c5dcc5` ledger chaining, `0dcb0e9` trace corpus mutation test, `497e91b` fuzzer,
`2c8e247` all-ports grader, `27d81ff` Makefile, `b61f237` layout.
Ports: `59d701b` TypeScript, `e5c3c9d` Rust, `9c62f3b` Zig, `aeb2357`/ `c7b2f3a`
tests, `9c2f069` fuzz fix.
