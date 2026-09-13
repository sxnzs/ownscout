# DeltaDB primitives, and what OwnScout took from them

This is the design record for the round that studied Zed's **DeltaDB** and folded
the transferable parts into OwnScout's evidence engine. The two research inputs
are [`research-deltadb.md`](research-deltadb.md) (public sources on DeltaDB) and
[`research-zed-anchors.md`](research-zed-anchors.md) (the open-source CRDT
substrate, read directly from the Zed checkout at `5a9b9558db`).

## 1. What was actually studied

DeltaDB is **closed beta**. There is no published spec, wire format, delta ID
scheme, read API, or GC policy, and the Zed checkout contains no `deltadb` crate
— it is mentioned once, in a comment saying a crate "is going away with DeltaDB
soon". So nothing here is derived from DeltaDB source. What *is* available is:

- Zed's public claims about DeltaDB (stable per-delta identity, references
  "anchored to a delta instead of a line number", conflict-free replicated
  worktrees, log-first storage with checkpoints), and
- the **open-source `crates/text` CRDT** that the collaboration layer is built
  on, which is the only real anchor implementation to read.

That distinction matters: the marketing claim and the mechanism turned out to
disagree in an important way (§3).

## 2. How a Zed anchor actually works

An `Anchor` is not a position; it is a *name* for a position
(`crates/text/src/anchor.rs:11-28`):

```
(replica_id: u16, seq: u32, offset: u32 /* into that insertion */, bias, buffer_id)
```

Resolution is a two-index lookup — `(timestamp, split_offset) → Locator`, then
`Locator → visible byte offset` — over two `SumTree`s, so it costs O(log N) in
the number of fragments and never scans the text
(`crates/text/src/text.rs:2509-2543`, `:2584-2608`). Deletion is **retention,
not removal**: a deleted fragment keeps its identity and its bytes, is flagged
`visible = false`, and its bytes move to a `deleted_text` rope
(`crates/text/src/text.rs:1094-1099`, `:2977-2995`). Nothing is ever pruned, so
anchors never expire — and memory is unbounded.

The load-bearing property for us is *why* identity survives: the CRDT never
rewrites an insertion string, so `(timestamp, offset)` stays meaningful while
`Locator`s are reassigned on every edit. The anchor is an immutable key; the
current location is a derived, mutable index.

## 3. The finding that shaped the design

**Zed's CRDT has no move primitive.** `Operation` has exactly two variants,
`Edit` and `Undo` (`crates/text/src/text.rs:618-622`), and every insertion mints
a fresh Lamport timestamp (`:882`, `:1890`). A cut-and-paste is therefore
delete-at-source plus insert-at-destination, and the anchor stays on the *source*
tombstone: it resolves, reports `is_valid() == false`
(`crates/text/src/anchor.rs:150-168`), and **never follows the pasted content**.

So "references survive as the code moves underneath it" is true for *position
shift* (lines inserted above a reference) and false for *content relocation*.
The Zed research names the missing mechanism explicitly: supporting relocation
"requires either an explicit `Expired` anchor state or a **content-based
fallback locator** that does not depend on retained identity".

That is the primitive OwnScout adopted.

## 4. What OwnScout's contract forced

`packet-v1` is frozen and carries only `path`, `line_start`, `line_end`, and
`content_hash` for an evidence span. There is **no anchor origin and no copy of
the cited text**. Two consequences:

- The recorded SHA-256 fingerprint *is* the only identity available, and the
  recorded line count is the only shape. OwnScout's anchor is therefore a
  **weak, content-addressed anchor**, not an identity-keyed one.
- Relocation can only be a *search for a window whose fingerprint matches* —
  which is exactly the content-based fallback locator, and exactly the thing
  Zed's design lacks.

This is the honest framing: we did not port Zed's anchor, because packet-v1
cannot express one. We ported the fallback.

## 5. Primitives: adopted, adapted, rejected

| # | Primitive | Verdict for OwnScout |
|---|---|---|
| P6 | Dual verification: fingerprint **and** re-resolvable anchor | **Adopted**, in its weakest contract-compatible form: `evidence verify --relocate` |
| P7 | Fail loudly rather than claim success you cannot verify | **Already held.** Evidence is re-stat'ed and `os.SameFile`-checked; relocation never upgrades a failure |
| P8 | A semantic unit, not a keystroke | **Already held.** The unit is one verification record |
| P4 | Append-only log + periodic checkpoints | **Half held.** The ledger is the append-only log; checkpoints are unnecessary because the ledger is capped at 1 MiB |
| P1/P2 | Addressable events; anchor = `(origin-id, offset)` | **Deferred to a packet-v2.** Requires adding an anchor field to the frozen contract |
| P5 | Stable file identity across renames | **Deferred.** Relocation is currently confined to the cited file |
| P9 | Invertible index ("which records cite this line?") | **Deferred.** Needs a cross-record index that a stateless CLI does not have |
| P10 | Virtualized worktree, replicated, multi-machine | **Rejected.** An entire sync engine for a tool that must stay local, stateless, and dependency-free |
| P11/P12 | Scope-aware recording; repo-derived access | **Out of scope.** OwnScout has no recording layer and no ACL |

The rejections are the point as much as the adoption: DeltaDB's costs (a
synchronization engine, a server, per-participant checkouts, a retention
problem, and a documented absence of any delta GC policy) buy collaboration
that an evidence verifier does not need.

## 6. The adopted primitive

`ownscout evidence verify --relocate` re-resolves a failed span. When a span
fails because the range is invalid or the hash mismatched, and the recorded hash
is a well-formed SHA-256, the file is searched for a window **of the same line
count** whose normalized-range fingerprint equals the recorded one.

- **Probe order** is nearest-first from the cited start: the cited line, then
  one below, one above, and so on outward, preferring the lower line number on
  a tie. A small shift — the common case — is found in a handful of probes.
- **The origin is clamped** into the range of windows that actually fit, so a
  packet citing line 9000 of a 3-line file still searches the file instead of
  walking 9000 iterations.
- **A byte budget of 8 MiB** caps the total window bytes hashed per span. This
  is what stops re-resolution from degrading into an unbounded scan.
- **Three outcomes**, and the wording never overclaims:
  - found → `content relocates to lines A-B (shift +N; nearest matching window)`
  - not found, every fitting window probed → `content not found elsewhere in this file`
  - not found, budget exhausted first → `relocation search stopped after its byte budget`

The qualifier *nearest matching window* is deliberate: a content-addressed
anchor is ambiguous when a file contains duplicated content, which is an
inherent cost of the approach (§7).

**Relocation is diagnostic only.** It never changes a span's status, the report
counters, the exit code, or any ledger byte. A span whose content moved stays
`failed` and the command still exits 1. There is deliberately no path by which
relocation turns a failure into a pass.

## 7. What this cannot do (stated, not glossed)

- **Ambiguity.** Duplicated windows yield many matches; the nearest is reported.
- **No cross-file relocation.** A move to another file reads as "not found
  elsewhere in this file".
- **Edits inside the span break the identity.** If the content moved *and* was
  edited, no window of that shape matches and the search reports absence.
- **Absence is bounded by the budget.** For a large file, "not found" may mean
  "not found within the budget"; the three message variants exist to keep that
  distinction visible rather than collapsing it into a false negative.
- **No lineage guard.** Zed's `buffer_id` exists so a reference from a replaced
  buffer is *refused* rather than silently re-homed. OwnScout has no equivalent,
  so a span could in principle be "relocated" to unrelated identical content.

## 8. Cost model (measured, `go test ./internal/evidence -bench BenchmarkRelocate`)

The search must not re-walk the file per probe. `hashSelectedLines` walks from
byte zero, so using it per candidate would cost O(probes × file size): measured
at **98 ms for 275 probes on a 6.3 MB file**. Indexing line starts once and
hashing windows from recorded offsets removes that factor — **275 probes in
272-341 µs, independent of file size**.

The honest total, however, is **one pass over the file per resolved span** to
build the index, because the index itself is O(lines). The benchmarks show it:

| File | Nearby shift (extent 16) | Absent content |
|---|---|---|
| 64 KiB | 63 µs | — |
| 1 MiB | 447 µs | 4.1 ms |
| 8 MiB | 3.8 ms | — |

So relocation is ~2.2 GB/s of file, paid only for spans that already failed and
only under the flag. The index is also O(lines) transient memory (~8 bytes per
line), which is the real cost of the design; a packet with many failed spans in
one large file pays the pass once per span, which is the same shape as the
existing per-span file read.

The absent-content case is where the byte budget earns its keep: it stops at
~4 ms instead of scanning the file without bound.

## 9. Evidence and gates

- A differential test pins the relocation path's window hashing against the
  frozen `hashSelectedLines` for every range of 24 awkward payloads (CRLF, a lone
  `\r`, invalid UTF-8, blank lines, an unterminated tail, a trailing `\r` with no
  `\n`).
- The edge corpus grew by thirteen cases covering moved / gone / shrunken-file /
  budget-stop / flag-off / flag-rejected / trailing-CR, so every branch and every
  message variant is an oracle the three ports must replay byte-for-byte.
