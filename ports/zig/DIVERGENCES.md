# Zig port divergences

Divergences from the Go reference that this port does not reproduce. None is
covered by `corpus.json` or `corpus-edge.json`, so both recorded corpora pass.
They are recorded here rather than silently approximated, per
`spec/parity/PORT.md`.

The two decoding gaps this file previously recorded, and the ledger-validation
soundness gap, are fixed.

## 1. Append failures report the Open wording

The reference distinguishes a failure to open the ledger
(`ledger could not be opened` / `ledger open failed`) from a failure to append
(`ledger append failed` / `node results could not be appended`). This port maps
every ledger error to the Open wording, so an append-time failure would report
the wrong summary. Not reachable through the corpora.

## 2. Ledger filesystem hardening is not ported

`internal/ledger/ledger.go` `Open` rejects symlink ancestors (with a `/var`
exception), requires a link count of 1, checks parent identity and resolved-path
equality, and takes a `flock`. This port only rejects a ledger path inside the
repository. That single rejection is the pinned case; the rest is unverified.

## 3. Append-time size cap is not enforced

The reference rejects an append that would push the ledger past 1 MiB. This port
enforces the record-size cap but not the whole-ledger append-time cap.

## 4. Per-line scanner bound is approximated

The reference bounds each scanned line with a `bufio.Scanner` buffer. This port
derives the bound from the trimmed record length, which is equivalent for the
shapes tested but is not the same computation.
