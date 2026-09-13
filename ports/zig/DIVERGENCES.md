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
`strings.EqualFold` field check. The ledger append path now rejects symlink
ancestors (with the macOS `/var` system-link exception), pinned by the
`node-edge-ledger-symlink-ancestor-*` cases, while `ledger verify` keeps
following links; the residual `Open` hardening (link count 1, parent identity,
resolved-path equality, `flock`) is still not ported and has no corpus case.

## 1. Uncommon packet read failures keep a generic detail

`internal/cli/packet.go` `readBounded` returns the underlying OS error. The
shapes the reference actually produces now match byte-for-byte: a directory
input (`read packet "d": read d: is a directory`), permission denied
(`open packet "p": open p: permission denied`), a missing file
(`packet file "<path>" does not exist`) and the 1 MiB cap
(`packet exceeds 1048576 byte input limit`, reported as the loaded shape, not a
decode failure). An OS read error outside those shapes (an I/O error) is
collapsed to `read packet "<path>": read <path> failed` instead of the strerror
text; no corpus case reaches it.

## 2. Append failures report the Open wording — fixed

The reference distinguishes a failure to open the ledger
(`ledger could not be opened` / `ledger open failed`) from a failure to append
(`ledger append failed` / `node results could not be appended`). ~~This port maps
every ledger error to the Open wording.~~

Fixed in `6547bf2`: `appendLedger` returns distinct `LedgerOpenFailed`,
`LedgerAppendFailed` and `LedgerFull` errors and `nodeVerify` maps each to the
matching summary. Still not reachable through the corpora.

## 3. Append-time size cap — enforced, but unpinned

~~The reference rejects an append that would push the ledger past 1 MiB. This port
enforces the record-size cap but not the whole-ledger append-time cap.~~

Fixed in `6547bf2`: `appendLedger` checks `output.items.len > ledger_max_size`
and returns `error.LedgerFull`, which `node verify` surfaces as `ledger is
full`. The *open-time* cap is pinned by `ledger-edge-verify-full-*` and
`node-edge-ledger-full-*`. **The append-time cap itself has no corpus case** —
`ledger-full.jsonl` is over-cap before `Open`, so `Append`'s overflow path is
unexercised. Pinning it needs a `node verify` case against a valid ledger sized
just under 1 MiB.

## 4. Per-line scanner bound is approximated

The reference bounds each scanned line with a `bufio.Scanner` buffer. This port
derives the bound from the trimmed record length, which is equivalent for the
shapes tested but is not the same computation.
