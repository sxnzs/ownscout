# OwnScout

OwnScout is an owned, local-first repository evidence CLI. It validates compact
evidence packets, verifies evidence spans against the repository, and gives
humans, developers, and agents one predictable interface.

## Status

Experimental v0. The first release intentionally supports:

- packet contract validation
- evidence span verification
- deterministic JSON and human output
- no daemon, no paid search service, and no silent indexing

## Install

```bash
go build -o bin/ownscout ./cmd/ownscout
```

## Commands

```bash
bin/ownscout doctor
bin/ownscout contract validate --packet packet.json
bin/ownscout evidence verify --repo /path/to/repo --packet packet.json
```

Use `--json` for deterministic machine output.
