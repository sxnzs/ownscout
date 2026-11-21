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
- The Go reference in `internal/` is the spec; ports under `ports/` replay the corpus byte for byte. Keep the repo `npm`-free.
- Any CLI stdout or error-string change needs `make corpus` and the regenerated corpus committed.
- Never hand-edit the mutation table in `spec/parity/PORT.md`; run `make mutants` (new mutations go in `tools/mutants/main.go`).
- `spec/parity/` is protected: port lanes fail on any untracked or modified file outside `ports/`. Keep scratch in `mktemp -d`, never `/tmp` (symlinked; ledger paths there are rejected).
- Packets decode only through `internal/nodepacket` (strict, 1 MiB cap); the ledger is capped at 1 MiB and a full ledger is a rotation point.
- Other agents may work here concurrently: keep commits scoped.
