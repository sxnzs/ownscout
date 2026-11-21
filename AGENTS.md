# Working in this repo

## Before you edit
- For anything larger than a small fix, write a short spec first: goal, files touched, how we will know it works. Wait for my OK.
- Read the code and `git log` before trusting docs or your memory of a tool.

## While you work
- Build at low or medium effort. Switch to high for debugging, security-sensitive code, and final verification.
- Higher effort finds missed edge cases. It does not fix a wrong spec. If the approach looks wrong, go back to the spec.
- Do not add lint-disable or type-ignore comments. If a check is wrong, stop and tell me.
- Leave touched code cleaner: no dead code, no comments that restate the code.

## Done means
- `./scripts/verify` exits 0, and CI is green on the pushed commit. Never tell me something works without that.
- Never weaken, skip, or delete a check or test to get to green.
- Commit and push as you go, in small commits.

## When I correct you
- Append one line to `.corrections` naming the class of mistake, not the instance.
- If that class is already listed, do not add another line. Propose a lint rule, type, or compiler check that makes it impossible; once I approve, add it to `scripts/verify`.
- Do not add rules to this file. It stays under 30 lines.

## Repo facts
- OwnScout is one Rust crate at the repo root; `spec/parity/corpus*.json` and `fixtures/` are the frozen behaviour spec the harness replays byte for byte.
- Any CLI stdout or error-string change must update the corpus JSON in the same commit, and that corpus edit needs my review.
- Keep scratch in `mktemp -d`, never `/tmp` (symlinked on macOS; ledger paths there are rejected).
- Packets decode strictly (exact field names, duplicate keys rejected, 1 MiB cap); the ledger is capped at 1 MiB and a full ledger is a rotation point.
- No `npm`. Other agents may work here concurrently: keep commits scoped.
