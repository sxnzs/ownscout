# TypeScript port divergences

Divergences from the Go reference that this port does not reproduce. Neither
corpus covers them, so both recorded corpora pass. They are recorded here rather
than silently approximated, per `spec/parity/PORT.md`.

## 1. The ledger is not locked while it is open for append

`internal/ledger/ledger.go` `Open` takes an advisory exclusive `flock` and holds
it for the store's lifetime, so two concurrent `node verify` runs cannot
interleave appends. Node's standard library has no file-locking primitive, and
this repository stays dependency-free and `npm`-free, so the TypeScript port
reads and renames without a lock.

The gap is unobservable in the trace corpus, which is single-process: every case
sees one writer. It is real under concurrency - two simultaneous appends could
interleave - and it is not fixable without a native dependency, so it is recorded
rather than left implicit.

## 2. `O_NOFOLLOW` on the ledger's final component

The reference opens the ledger with `O_NOFOLLOW`, so a symlink at the final path
component is refused by the kernel. This port reaches the same outcome through an
`lstat` check and a "not a regular file" refusal, which is what the pinned case
records, but it is a check-then-open rather than an atomic one.

The window is a race between the two calls, not a difference in the ordinary
outcome.
