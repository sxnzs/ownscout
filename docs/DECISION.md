# OwnScout v0 decision

## Decision

Build an isolated Go CLI, not an MCP server or daemon.

## Why

- Go produces a fast static binary and has excellent local DX.
- Standard-library-first implementation minimizes dependency and supply-chain risk.
- A CLI is usable by humans, shell scripts, editors, Codex, and future MCP adapters.
- The packet contract is the trust boundary, so it must exist before indexing/model routing.
- No daemon avoids background state drift and accidental indexing.

## UX / DX / AX rule

Every command must:

- be safe by default,
- support human output,
- support deterministic `--json` output,
- use stable exit codes,
- explain the next action on failure,
- never mutate the repository unless an explicit command says so.

## Consequences

- MCP is deferred until the CLI and contract are stable.
- Evidence verification precedes autonomous use.
- Invalid, stale, failed, and unavailable evidence have distinct outcomes.
