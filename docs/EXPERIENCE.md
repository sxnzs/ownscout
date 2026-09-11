# OwnScout CLI experience

OwnScout is one predictable local command surface for humans, scripts, editors,
and accessibility tools.

## Packet boundary

OwnScout accepts packet-v1 JSON only. Decoding is strict: unknown fields are
rejected; no compatibility normalization is performed; and hashes are never
silently repaired. Fields explicitly modeled by the packet contract, including
its documented compatibility spellings, are accepted. A packet must be fixed
at its source rather than reinterpreted by the CLI.

## Safe workflow

Validate first, verify evidence second, and consume only after both commands
return exit code `0`:

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

Use `--json` for automation. Omit it for human diagnosis: the default output
is plain text and includes the next action.

## UX rules

- Behavior is safe and local-only: no daemon, network access, or repository
  mutation.
- Human-readable output is concise, plain text, and never prints packet
  contents.
- Every failure names the problem and gives a concrete `Next action:`.
- `--help` works at the root and at every command level.
- Unknown commands, subcommands, flags, and missing flag values fail clearly.

## DX rules

- The command vocabulary is small and stable:
  - `ownscout doctor`
  - `ownscout version`
  - `ownscout contract validate --packet <file> [--json]`
  - `ownscout evidence verify --repo <dir> --packet <file> [--json]`
  - `ownscout node verify --repo <dir> --packet <file> --envelope <file> --ledger <file> [--json]`
- JSON mode is one line with stable fields:
  `command`, `ok`, `summary`, `details`, `next_action`.
- Exit codes are suitable for shell automation and do not depend on output mode.
- Packet and repository paths are explicit; there is no implicit current-project
  scan.
- `node verify` also requires an explicit envelope and ledger path. It appends
  once, including failed and blocked node results, and keeps the ledger outside
  the repository.

## AX rules

- Labels, actions, and statuses (`OK`, `ERROR`) are plain text and
  screen-reader-friendly.
- Output is short, line-oriented, and understandable without color.
- JSON consumers can ignore presentation and use the stable result fields.
- Errors do not require seeing or interpreting a raw packet dump.

## Exit codes

| Code | Meaning |
|---:|---|
| 0 | Success, valid packet, or verified evidence |
| 1 | Contract/evidence failure, or graph failure after ledger append |
| 2 | Usage, packet/envelope parse or validation, repository/ledger I/O, or append failure |

## Output examples

Human output:

```text
OK: packet is valid
  - outcome: complete
  - default action: autonomous_proceed
Next action: Run evidence verification before consuming this packet.
```

JSON output:

```json
{"command":"contract validate","ok":true,"summary":"packet is valid","details":["outcome: complete","default action: autonomous_proceed"],"next_action":"Run evidence verification before consuming this packet."}
```

Failure output:

```text
error: missing required --packet <file>
Next action: run 'ownscout contract validate --help'.
```

## Non-goals

OwnScout does not index repositories, run as a background service, call a
network or paid search API, change repository files, or replace the packet
contract with an unstructured report. Evidence verification is intentionally
local and bounded to the paths declared by the packet.

## Implementation boundary

The CLI validates the canonical packet, then checks its declared evidence spans
against the supplied working-tree root. It does not consume a packet after a
failed validation or verification step.

## Node verification guidance

`node verify` is the auditable graph path for `node-envelope-v1`. For humans,
plain output lists each node in evaluation order and gives a next action. For
scripts and agents, use `--json`; the five-field envelope is stable and node
status details preserve order. The command never prints packet or envelope
contents, never trusts producer verifier statuses, and never mutates the
repository. Invalid input stops before ledger creation; once the ledger opens,
it is closed even when evidence or graph evaluation fails.
