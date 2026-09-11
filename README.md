# OwnScout

OwnScout is a local-first CLI for validating evidence packets and checking their
repository spans. It is standard-library-only, deterministic, and safe by
default: it does not start a daemon, use the network, or mutate repositories.

## Packet format

OwnScout accepts packet-v1 JSON only. Decoding is strict: unknown fields are
rejected, no compatibility normalization is performed, and hashes are never
silently repaired. Fields explicitly modeled by the packet contract, including
its documented compatibility spellings, are accepted. Fix the packet instead
of expecting the CLI to reinterpret it.

## Quickstart

```bash
go build -o bin/ownscout ./cmd/ownscout
bin/ownscout doctor
bin/ownscout contract validate --packet packet.json
bin/ownscout evidence verify --repo /path/to/repo --packet packet.json
bin/ownscout node verify --repo /path/to/repo --packet packet.json \
  --envelope envelope.json --ledger /path/to/ownscout-ledger.jsonl
```

For release builds, inject the version:

```bash
go build -ldflags "-X ownscout/internal/cli.version=v0.1.0" -o bin/ownscout ./cmd/ownscout
```

Add `--json` to any validation command for one-line machine-readable output.
Every JSON result has `command`, `ok`, `summary`, `details`, and `next_action`.
Run `ownscout --help` or append `--help` to a command for usage.

`node verify` strictly decodes both inputs, binds the envelope to OwnScout's
canonical packet digest, checks fresh repository evidence, evaluates nodes in
deterministic dependency order, and appends the ordered results to the ledger.
The ledger must be outside the repository. Failed and blocked node results are
recorded too; the command returns `1` after a successful append when the graph
does not pass, and `2` for input, repository, ledger, or append errors.

## Safe workflow

Validate first, verify evidence second, and consume the packet only when both
commands succeed:

```bash
if bin/ownscout contract validate --packet packet.json --json && \
   bin/ownscout evidence verify --repo /path/to/repo --packet packet.json --json; then
  # Consume packet.json here.
  :
else
  # Do not consume the packet.
  exit 1
fi
```

Use JSON mode in scripts and agents. Omit `--json` when a person is diagnosing
a failure; human output includes the concrete next action.

## Exit codes

- `0`: success, valid packet, or verified evidence
- `1`: contract failure, evidence failure, or node graph failure after append
- `2`: usage error, malformed node input, missing input file, repository/ledger
  I/O, or ledger append failure
