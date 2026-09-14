# OwnScout language ports

Three independent, standard-library-only ports of the Go reference in `internal/`,
all verified byte-for-byte against the same recorded trace corpus.

## Status (2026-09-13)

| Port | Binary | Base traces | Edge traces | Tests | Third-party deps |
|---|---|---|---|---|---|
| TypeScript | `ports/ts/bin/ownscout` | 31/31 | 245/245 | 149 | none (Node built-ins only) |
| Zig | `ports/zig/zig-out/bin/ownscout` | 31/31 | 245/245 | 37 | none (`.dependencies = .{}`) |
| Rust | `ports/rust/target/release/ownscout` | 31/31 | 245/245 | 38 | none (empty `[dependencies]`) |

The Go reference's observable behaviour is unchanged: 77 tests,
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

- Base trace corpus: 28 recorded CLI cases (human and `--json`, exit codes 0/1/2, ledger).
- Edge trace corpus: 245 cases (path escape, non-UTF8, graph cycles, ledger hash
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
- Trace corpus mutation test: four deliberately broken reference builds fail the
  trace corpora (base 26/28, 20/28, 28/28 and 28/28; edge 172/208, 106/208, 206/208 and
  206/208 — fractions as recorded against the 28+208 corpus those builds
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
`contract-edge-precedence-*`). All three ports pass 245/245 and fuzz seed 13 is
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

Current reading: `cmd` 100%, `cli` 92.0%, `contract` 89.3%, `evidence` 87.1%,
`ledger` 72.1%, `node` 83.8%, `nodepacket` 83.5%, with three functions never
executed: the retired `evidence.VerifyPacket` wrapper, `ValidationError.Unwrap`,
and `ledger.isPathPrefix`. `ledger` is the low one, and it is also where every gap of this shape
has been found, so it is the first place to look next.

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
