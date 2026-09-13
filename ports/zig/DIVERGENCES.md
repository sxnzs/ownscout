# Zig port divergences

Divergences from the Go reference that this port does not reproduce. None is
covered by `corpus.json` or `corpus-edge.json`, so both recorded corpora pass.
They are recorded here rather than silently approximated, per
`spec/parity/PORT.md`.

The two decoding gaps this file previously recorded, and the ledger-validation
soundness gap, are fixed. The case-folding divergence is gone: every command now
decodes through the one strict `internal/nodepacket` boundary, so the packet
decoders agree on exact names, duplicate keys, explicit null, invalid UTF-8 and
the 1 MiB input cap. `gofold.zig` remains only for the ledger's
`strings.EqualFold` field check.

## 1. Packet open/read failures use a fixed detail

`internal/cli/adapter.go` `readBounded` returns the underlying OS error, so the
reference prints e.g.
`open packet "p": open p: permission denied` or
`read packet "d": read d: is a directory`. This port collapses every non-missing
read failure to `read packet "<path>" failed`. A missing file
(`packet file "<path>" does not exist`) retains the loaded shape. The 1 MiB cap
is a strict decode failure and uses the generic decode response.

## 2. Append failures report the Open wording

The reference distinguishes a failure to open the ledger
(`ledger could not be opened` / `ledger open failed`) from a failure to append
(`ledger append failed` / `node results could not be appended`). This port maps
every ledger error to the Open wording, so an append-time failure would report
the wrong summary. Not reachable through the corpora.

## 3. Ledger filesystem hardening is not ported

`internal/ledger/ledger.go` `Open` rejects symlink ancestors (with a `/var`
exception), requires a link count of 1, checks parent identity and resolved-path
equality, and takes a `flock`. This port only rejects a ledger path inside the
repository. That single rejection is the pinned case; the rest is unverified.

## 4. Append-time size cap is not enforced

The reference rejects an append that would push the ledger past 1 MiB. This port
enforces the record-size cap but not the whole-ledger append-time cap.

## 5. Per-line scanner bound is approximated

The reference bounds each scanned line with a `bufio.Scanner` buffer. This port
derives the bound from the trimmed record length, which is equivalent for the
shapes tested but is not the same computation.
