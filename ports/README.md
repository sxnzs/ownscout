# OwnScout language ports

Three independent, standard-library-only ports of the Go reference in
`internal/`: TypeScript (`ports/ts`), Rust (`ports/rust`) and Zig
(`ports/zig`). All three reproduce the reference CLI's stdout, exit codes and
ledger bytes exactly, graded against the recorded oracle in `spec/parity/`.

The contract every port must satisfy is `spec/parity/PORT.md`; the recorded
status table and verification evidence are in `docs/PORTS.md`.

## Requirements

| Tool | Version used | Needed for |
|---|---|---|
| Go | 1.26+ | the reference CLI and the corpus tools |
| Node | 24 (runs `.ts` directly via type stripping) | TypeScript port |
| Rust | stable, edition 2021 | Rust port |
| Zig | 0.16.0 | Zig port |
| Python | 3.8+ | the parity harness |

## Build

TypeScript has no build step: `ports/ts/bin/ownscout` executes the source
with Node. Rust and Zig must produce the binaries the verifier expects:

```bash
(cd ports/rust && cargo build --release)   # ports/rust/target/release/ownscout
(cd ports/zig  && zig build)               # ports/zig/zig-out/bin/ownscout
```

Build both of them from the repository root with:

```bash
make ports
```

## Use

Each binary is a drop-in replacement for the Go CLI described in the
[top-level README](../README.md): the commands, flags, JSON shape and exit codes
are identical. Substitute any port binary for `bin/ownscout`:

```bash
ports/rust/target/release/ownscout doctor
ports/rust/target/release/ownscout contract validate --packet packet.json --json
ports/rust/target/release/ownscout evidence verify --repo /path/to/repo --packet packet.json
ports/rust/target/release/ownscout node verify --repo /path/to/repo --packet packet.json \
  --envelope envelope.json --ledger /path/to/ownscout-ledger.jsonl
```

## Tests

```bash
(cd ports/ts   && node --test)      # 106 tests
(cd ports/rust && cargo test)       # 17 tests
(cd ports/zig  && zig build test)   # 16 tests
```

## Verify

```bash
make gate                     # Go reference tests + corpus freshness
make ports                    # build the Rust and Zig binaries
spec/parity/verify-all.sh     # both corpora + each port's tests + protected paths
spec/parity/status.sh         # compact per-port status, never fails
make fuzz                     # differential fuzzing vs a freshly built reference
```

`verify-all.sh` grades every port against both corpora (28 base cases, 103 edge
cases), runs that port's own test suite, and then fails if anything outside
`ports/` was modified. Commit unrelated changes before running it.

The oracle itself is mutation-tested: three deliberately broken reference builds
fail the corpora, so a passing harness is meaningful rather than vacuous. See
`docs/PORTS.md` for those numbers.
