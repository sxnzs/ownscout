# TypeScript port divergences

Divergences from the Go reference that this port does not reproduce. Neither
corpus covers them, so both recorded corpora pass. They are recorded here rather
than silently approximated, per `spec/parity/PORT.md`.

## 1. The ledger is not locked while it is open for append

`internal/ledger/ledger.go` `Open` takes an advisory exclusive `flock` and holds
it for the store's lifetime, so two concurrent `node verify` runs cannot
interleave appends. Node's standard library has no file-locking primitive, and
this repository stays dependency-free and `npm`-free, so the TypeScript port
holds one descriptor for the store's lifetime but takes no lock.

The gap is unobservable in the trace corpus, which is single-process: every case
sees one writer. It is real under concurrency - two simultaneous appends could
interleave - and it is not fixable without a native dependency, so it is recorded
rather than left implicit.
