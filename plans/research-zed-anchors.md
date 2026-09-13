# Zed CRDT text primitives: how a reference survives edits

Source: `/Users/sainzs/Code/reference/zed` at commit `5a9b9558db01a6b906cec2fb70a797affdc58cdd` ("Use proper editions (#63733)"). Read-only inspection; nothing in that checkout was modified. All paths in this report are relative to that checkout root.

---

## 0. Scope note: where "DeltaDB" actually is (important)

**DeltaDB is not present in this checkout.** A full-tree search for `DeltaDB`/`delta_db`/`deltadb` finds only:

- `crates/collab/tests/integration/random_project_collaboration_tests.rs:1276` — a comment: *"This whole crate is going away with DeltaDB soon, so we hold our nose and continue."*
- `crates/vim/src/surrounds.rs:513,520,1363,1371,1413,1421` — `'DeltaDB'` is only test-fixture text inside surround-pair tests.

There is no `delta`/`deltadb` crate and no version-control code in the tree that consumes `text::Anchor` (the `git` crate has no anchor usage; `crates/git/src/blame.rs` is ordinary git blame). `crates/db` is a SQLite wrapper.

**Consequence for this report:** the primitives described below are the `crates/text` buffer CRDT (`Anchor`, `Locator`, `Buffer`/`BufferSnapshot`, `Patch`, `OperationQueue`) and its acceleration structures (`rope`, `sum_tree`). Those are the primitives the comment says DeltaDB supersedes *in the collaboration layer*, and they are the only anchor mechanism that exists at this commit. Where I make a claim about how a hypothetical DeltaDB would use them, I label it as inference. Everything in §2–§6 is grounded in code that exists at this commit.

A second scope caveat: above `crates/text` there is a *composite* anchor layer (`crates/multi_buffer/src/anchor.rs`, `Anchor::Buffer`/`Anchor::Excerpt`) whose ordering/validity bugs were fixed in commit `49469000f0` ("Ensure that multibuffer anchor ordering is stable over time and consistent with resolution (#62636)"). That commit message is quoted in §5 because it names the invariants precisely; it is a git-log citation, not a file:line citation.

---

## 1. Summary

A Zed text **`Anchor` is not a position; it is a name for a position**. Concretely it is a 3-tuple of *(insertion identity, byte offset inside that insertion, bias)* plus a buffer id (`crates/text/src/anchor.rs:11-28`). The insertion identity is a Lamport timestamp `(replica_id, seq)`; the offset is a byte offset into the string that the operation which produced that insertion put into the buffer, not into the current rope (`crates/text/src/anchor.rs:21-23`).

Resolution is a **two-index lookup**: `(timestamp, offset)` → `Locator` (current document-order key) via the `insertions` tree, then `Locator` → current visible byte offset via the `fragments` tree, adding the in-fragment overshoot if the fragment is still visible (`crates/text/src/text.rs:2509-2543`, `:2584-2608`). Both trees are `sum_tree::SumTree` — B+ trees with cached subtree summaries — so resolution is O(log N) in the number of fragments (`crates/sum_tree/src/sum_tree.rs:206-213`, `:450-507`; `crates/sum_tree/src/cursor.rs:465-...`).

`Locator` is an order-maintenance label (a `SmallVec<[u64; 2]>` fraction between two neighbours, `crates/text/src/locator.rs:12`, `:52-65`). It exists because the CRDT needs two *different* orderings over the same material — insertion order (for `(timestamp, offset)` lookup) and document order (for offset lookup) — and needs to be able to insert into either without renumbering. It is **never serialized**: it has no serde derives, no protobuf message, and no out-of-crate consumers (`grep` for `Locator` outside `crates/text/src` matches only unrelated `SymbolLocator`/`DebugLocator` names). Anchors *are* serialized, as 5 proto fields (`crates/proto/proto/buffer.proto:238-244`, `crates/language/src/proto.rs:265-278`, `:529-550`).

Deletion is **retention, not removal**. A deleted range keeps its `Fragment`, its Lamport identity and its bytes (moved into a `deleted_text` rope); it is only flagged `visible = false` with the deleting edit recorded in `deletions` (`crates/text/src/text.rs:569-577`, `:1092-1099`, `:2011-2032`, `:1141-1148`, `:2061-2068`). So an anchor into deleted text still *resolves* (to the visible offset where the deleted run began, `crates/text/src/text.rs:2537-2540`) but reports `is_valid() == false` (`crates/text/src/anchor.rs:150-168`).

There is **no move operation**. `Operation` has exactly two variants, `Edit` and `Undo` (`crates/text/src/text.rs:618-622`). A cut-and-paste is therefore delete-then-insert: the paste receives a *new* Lamport timestamp and thus a new fragment identity. The design can say "the content at this reference is currently deleted" but it cannot say "the content at this reference moved over there".

Anchor identity survives arbitrary edits because the CRDT never rewrites the *insertion string*: every fragment records `(timestamp, insertion_offset)` into a string that is append-only-by-construction, and splits merely create more `(timestamp, split_offset)` keys into it (`crates/text/src/text.rs:605-610`, `:1864-1905`). Resolution is a query over that index, not over the current text.

The price is that the fragment/insertion index is **never pruned**: `fragments` and `deleted_text` grow with total edit history, and no GC exists in the crate (the only `truncate` is on the *undo stack*, `crates/text/src/text.rs:335`). Anchors therefore never expire — but only because nothing is ever collected.

---

## 2. Mechanism, file by file

### 2.1 `crates/text/src/anchor.rs` (249 lines)

**What it is.** Doc comment: *"A timestamped position in a buffer."* (`:8`). Struct (`:10-28`):

```rust
pub struct Anchor {
    pub(crate) timestamp_replica_id: clock::ReplicaId,
    pub(crate) timestamp_value: clock::Seq,
    pub offset: u32,      // byte offset into the text inserted at `timestamp`
    pub bias: Bias,       // attach to char before/after the offset
    pub buffer_id: BufferId,
}
```

The replica id and sequence number are stored inline rather than as a `clock::Lamport` *"to avoid the alignment of our fields from increasing the size of this struct. This saves 8 bytes, by allowing replica id, value and bias to occupy the padding"* (`:14-17`). `Anchor` is `Copy + Clone + Eq + PartialEq + Hash` (`:10`) — deliberately cheap to put in maps/sets and to send over RPC (see §2.2).

**Creation from a position.** `BufferSnapshot::anchor_at(position, bias)` (`crates/text/src/text.rs:2630-2632`) converts the position to a byte offset then calls `anchor_at_offset` (`:2634-2669`), which is where the *representation* is decided:

- `bias == Left && offset == 0` → the **min sentinel** `Anchor::min_for_buffer` (`:2635-2636`, sentinel built at `anchor.rs:59-67` from `Lamport::MIN`, `offset = u32::MIN`, `Bias::Left`).
- `bias == Right && offset == len` → the **max sentinel** (`:2637-2640`; `anchor.rs:69-77`).
- otherwise: round the offset to a char boundary in the direction of the bias (`:2642-2650`), find the visible fragment containing that offset (`:2651`), and build a real anchor from `fragment.timestamp` and `fragment.insertion_offset + overshoot` (`:2661-2668`).

So there are three kinds of anchor: min, max, and real. `is_min`/`is_max` are pure value predicates (`anchor.rs:170-180`), and min/max are *sticky*: an anchor created at buffer start with left bias stays at offset 0 no matter how much text is prepended, because it is a sentinel and is special-cased at resolution (`text.rs:2510-2513`). `Anchor::bias_left`/`bias_right` (`anchor.rs:128-140`) convert a real anchor to its other-biased form by *resolving it to an offset and re-anchoring* (`buffer.anchor_before(self)` / `anchor_after(self)`), i.e. re-biasing can change the anchor's identity, not just its bias bit.

**Resolution back to a position.** `ToOffset for Anchor` → `BufferSnapshot::offset_for_anchor` (`text.rs:3431-3437`, `:2509-2543`), and `ToPoint`/`ToPointUtf16`/`ToOffsetUtf16` for `Anchor` are implemented on top of it (`text.rs:3463-3494`, `:3495-3526`, `:3527-3540`). The `FromAnchor` trait (`text.rs:3548-3578`) is the generic hook that turns any anchor into `usize`/`Point`/`PointUtf16` for the surrounding editor code. The algorithm:

1. `is_min` → `0`; `is_max` → `visible_text.len()` (`:2510-2513`).
2. `debug_assert_eq!(anchor.buffer_id, self.remote_id)` and
   `debug_assert!(self.version.observed(anchor.timestamp()))` — *"Anchor timestamp {:?} not observed by buffer {:?}"* (`:2515-2521`).
3. `try_find_fragment(anchor)` → the `InsertionFragment` whose insertion-key contains `(timestamp, offset)` (`:2522-2527`, helper at `:2584-2608`). If the found insertion's timestamp differs from the anchor's, `panic_bad_anchor` (`:2523-2527`, `:2545-2563`).
4. `fragments.find::<Dimensions<Option<&Locator>, usize>>(None, Some(&insertion.fragment_id), Bias::Left)` → `(start, _, item)`; `start.1` is the count of *visible* bytes before this fragment (`:2529-2537`).
5. if the fragment is visible, add `anchor.offset - insertion.split_offset` (the overshoot inside the fragment); if it is **not** visible, add nothing (`:2538-2541`).

`fragment_id_for_anchor` (`:2565-2568`) is the panicking variant, used for ordering.

**Ordering.** `Anchor::cmp` (`anchor.rs:91-103`) compares, in order: fragment id in the current document (skipped if timestamps are equal), then `offset`, then `bias`. This makes anchor order equal to document order *within one snapshot* without resolving to offsets — and it is the mechanism that made commit `49469000f0` necessary (see §5). Note that fragment-id comparison requires the anchor to be resolvable, so `cmp` on an unknown anchor panics through `fragment_id_for_anchor` → `panic_bad_anchor`.

**Range helpers.** `OffsetRangeExt` (`anchor.rs:203-226`) converts ranges; `AnchorRangeExt` (`anchor.rs:228-248`) gives `cmp`/`overlaps`/`contains_anchor` in anchor space. `anchor_range_inside` uses `anchor_after(start)..anchor_before(end)` so the range expands with insertions at its edges, and `anchor_range_outside` uses `anchor_before(start)..anchor_after(end)` so it contracts (`text.rs:2610-2618`). This is the *bias* mechanism used in practice for selections.

**Cheap identity.** `opaque_id()` (`anchor.rs:190-200`) packs `buffer_id` (8 bytes LE), `offset` (4), `timestamp_value` (4), `replica_id` (2), `bias` (1) into `[u8; 20]`, used as a stable GPUI `ElementId` (`crates/gpui/src/window.rs:7275-7276`, consumed e.g. at `crates/editor/src/display_map/block_map.rs:350`).

**Summary at an anchor.** `Anchor::summary::<D>` → `BufferSnapshot::summary_for_anchor` → `text_summary_for_range(0..offset_for_anchor(anchor))` (`anchor.rs:142-147`, `text.rs:2502-2507`), i.e. "summary of everything before this anchor". `summaries_for_anchors_with_payload` (`text.rs:2442-2500`) does the same for a whole sequence with one forward-moving pair of cursors, resolving each via `try_find_fragment` + `fragment_cursor.seek_forward` (`:2490-2497`).

### 2.2 `crates/text/src/locator.rs` (177 lines)

**What it is.** *"An identifier for a position in a ordered collection. Allows prepending and appending without needing to renumber existing locators using `Locator::between(lhs, rhs)`. The initial location for a collection should be `Locator::between(Locator::min(), Locator::max())`, leaving room for items to be inserted before and after it."* (`:4-10`). Representation: `pub struct Locator(SmallVec<[u64; 2]>)` (`:12`), i.e. a fraction/base-`2^64` digit string, compared lexicographically (`Ord` derive at `:11`; `Clone` hand-written to avoid `SmallVec` clone overhead, `:14-26`).

`min()`/`max()` are the constant sequences `[u64::MIN]` / `[u64::MAX]` with `len = 1` (`:29-37`).

**`between`.** (`:52-65`):

```rust
// This shift is essential! It optimizes for the common case of sequential typing.
let mid = lhs + ((rhs.saturating_sub(lhs)) >> 48);
location.push(mid);
if mid > lhs { break; }
```

It takes the midpoint in the top 16 bits of the current limb; if that yields no room (`mid == lhs`), it emits `lhs`'s limb and recurses to the next limb, padding the shorter side with `u64::MIN`/`u64::MAX` (`:53-54`). The `>> 48` is what keeps the common cases shallow (see tests in §2.6).

**Why it is separate from `Anchor`.** They answer different questions:

| | `Anchor` | `Locator` |
|---|---|---|
| Denotes | a *stable* insertion-relative point | a *current* occupancy slot in an ordered sequence |
| Stability | must survive arbitrary edits (that is its purpose) | may be reassigned whenever the item is touched |
| Ordering | document order, derived via lookup | intrinsic (lexicographic on the fraction) |
| On the wire | yes, protobuf (`crates/proto/proto/buffer.proto:238-244`) | no; reconstructed locally |
| Consumers | whole editor/language stack | only `crates/text` internals |

The crucial mechanical fact is that `Locator` *is reassigned* on edits, while the anchor's `(timestamp, offset)` is not. Every call site that rewrites a fragment mints a fresh Locator between the running maximum and the old id:

- `crates/text/src/text.rs:1047` — `prefix.id = Locator::between(&new_fragments.summary().max_id, &prefix.id);`
- `crates/text/src/text.rs:1093` / `:2016` — same for a deleted `intersection`
- `crates/text/src/text.rs:1889` — same for each newly inserted fragment in `push_fragments_for_insertion`
- `crates/text/src/text.rs:787` — sequential ids while materializing the base text

while the *insertion key* is preserved across those rewrites by adjusting `insertion_offset` back to an absolute offset within the insertion (`:1045`, `:1090`, `:997`, `:1018`, `:1129`, `:1955`, `:1975`, `:2013`, `:2050`). Untouched regions are grafted wholesale and keep their Locators (`crates/text/src/text.rs:1006-1009`, `:1138-1140`, `:1964-1966`, `:2058-2060`). So Locators are *usually* stable but must not be relied on; anchors name `(timestamp, offset)` and re-derive the Locator on every lookup.

**Encoding/serialization.** None. `locator.rs` has no `serde`/`Serialize`/`Deserialize` derives (verified by grep), no protobuf message, and `Locator` appears nowhere outside `crates/text/src/*` except unrelated same-named symbols. The wire encoding of a *reference* is the `Anchor` encoding, not the `Locator`. `Anchor` proto fields: `replica_id: uint32`, `timestamp: uint32`, `offset: uint64`, `bias: Bias`, `buffer_id: optional uint64` (`crates/proto/proto/buffer.proto:238-244`), written/read at `crates/language/src/proto.rs:265-278` and `:529-550` (note `deserialize_anchor` returns `None` if `buffer_id` is missing or zero — `BufferId` is `NonZeroU64`, `text.rs:70-72`, `:86-91`).

**How it is used.** As a key and as a dimension:

- `struct Locator` implements `sum_tree::Item` with `Summary = Locator` and `KeyedItem` with `Key = Locator` (`locator.rs:82-96`), and `ContextLessSummary` where `add_summary` *replaces* the accumulator (`:98-106`) — i.e. the "summary" of a run of locators is just the greatest one, which is exactly what `Dimension for Option<&Locator>` exploits (`text.rs:3292-3300`).
- `Fragment { id: Locator, ... }` (`text.rs:569-577`) — the ordered `fragments` tree is sorted by `id`.
- `InsertionFragment { timestamp, split_offset, fragment_id: Locator }` (`text.rs:605-610`) — the `insertions` tree is sorted by `(timestamp, split_offset)` and *carries* the Locator as payload. It is the join from stable identity to current position.
- `Edits` carries `range: Range<(&Locator, u32)>` (`text.rs:522`) and `has_edits_since_in_range` compares `fragment.id > *end_fragment_id` (`text.rs:2790-2793`).
- `check_invariants` asserts `fragment.id > prev_fragment_id` for every fragment, i.e. strict ascending document order (`text.rs:1745-1748`).

### 2.3 `crates/text/src/text.rs` (3844 lines), with `patch.rs` and `operation_queue.rs`

#### 2.3.1 The CRDT model

`BufferSnapshot` is the immutable state (`text.rs:112-124`):

```rust
visible_text: Rope, deleted_text: Rope,
fragments: SumTree<Fragment>,            // document order, keyed by Locator
insertions: SumTree<InsertionFragment>,  // insertion order, keyed by (timestamp, split_offset)
insertion_slices: TreeSet<InsertionSlice>, // edit_id -> deleted insertion ranges (for undo)
undo_map: UndoMap,
pub version: clock::Global,              // version vector
remote_id: BufferId, replica_id: ReplicaId, line_ending: LineEnding,
```

`Buffer` (`text.rs:59-68`) = latest `BufferSnapshot` + `history: History` + `deferred_ops: OperationQueue<Operation>` + a local `lamport_clock: Lamport`. `Buffer` derefs to `BufferSnapshot` (`text.rs:1907-1913`).

A `Fragment` (`text.rs:569-577`) is the atom:

```rust
struct Fragment {
    id: Locator,               // current document-order key (mutable across edits)
    timestamp: clock::Lamport, // immutable insertion identity
    insertion_offset: u32,     // absolute byte offset of this piece inside that insertion
    len: u32,
    visible: bool,             // false == tombstone
    deletions: SmallVec<[clock::Lamport; 2]>, // which edits deleted it
    max_undos: clock::Global,
}
```

`InsertionFragment { timestamp, split_offset, fragment_id }` (`text.rs:605-610`) is the secondary index; `InsertionFragmentKey { timestamp, split_offset }` (`text.rs:612-616`) is its total order. `MAX_INSERTION_LEN` (`text.rs:51-55`) caps a single insertion and is why `offset` and `len` are `u32`: *"Fragments larger than this will be split into multiple smaller fragments. This allows us to use relative `u32` offsets instead of `usize`, reducing memory usage."* In tests the cap is 16 bytes, in release `u32::MAX` (`:55`) — which is why the splitting tests in §2.6 exist.

#### 2.3.2 How edits become operations

`Buffer::edit` (`text.rs:870-890`):

1. `start_transaction()`, `let timestamp = self.lamport_clock.tick()` (`:881-882`) — the edit's own Lamport timestamp.
2. `apply_local_edit(edits, timestamp)` (`:892-903`) resolves input positions to offsets and calls `BufferSnapshot::apply_edit_internal` (`:1916-2070`), which returns `(EditOperation, Patch<usize>)`; the patch is published to subscribers (`:901`).
3. the operation is pushed to `history` (`:885`, see `History::push` at `:234-236`, a `TreeMap<Lamport, Operation>`), pushed onto the undo stack (`:886`), and the snapshot's version vector observes its own timestamp (`:887`).

`EditOperation` (`text.rs:624-630`):

```rust
pub struct EditOperation {
    pub timestamp: clock::Lamport,
    pub version: clock::Global,          // causal context: version BEFORE this edit
    pub ranges: Vec<Range<FullOffset>>,  // offsets into visible+deleted text
    pub new_text: Vec<Arc<str>>,
}
```

Two things matter for anchors. First, `version` is `self.version.clone()` captured *before* the edit (`text.rs:1924`), so it is the operation's causal dependency set. Second, `ranges` are in **`FullOffset`** space, not visible-offset space: `FullOffset`'s `Dimension` adds `visible + deleted` (`text.rs:3282-3290`), and `apply_edit_internal` computes `full_range_start = FullOffset(range.start + old_fragments.start().deleted)` (`:1970`, `:2040`). Full offsets let an edit name a location even in text that some replicas consider deleted, which is what makes concurrent application deterministic.

`UndoOperation { timestamp, version, counts: HashMap<Lamport, u32> }` (`text.rs:632-637`) is the other variant; `apply_undo` (`:1221-1274`) flips fragment visibility through the `UndoMap` and rewrites the ropes.

`Patch<T>(Vec<Edit<T>>)` (`crates/text/src/patch.rs:7-8`) is the coalesced, ordered, non-overlapping delta stream handed to consumers: `push` drops empty edits and `push_maybe_empty` merges an edit into the previous one when `last.old.end >= edit.old.start` (`patch.rs:55-73`); `compose` (`patch.rs:88-...`) rebases one patch against another. `operation_queue.rs` is the out-of-order buffer for remote ops: `OperationQueue(SumTree<OperationItem<T>>)` keyed by Lamport (`operation_queue.rs:13-16`), `insert` sorts and dedups by timestamp before `tree.edit` (`:49-58`), `drain` empties it (`:60-64`), and its summary `add_summary` *asserts* keys are strictly increasing (`:79-83`) — the queue is an ordered set, not a bag.

#### 2.3.3 Applying remote edits (and what happens to fragments)

`apply_ops` (`text.rs:909-922`) pushes each op into history, and applies it only if `can_apply_op` says its causal version is observed (`:1290-1299`: `self.version.observed_all(&op.version)` and the replica is not blocked); otherwise the op is deferred into `OperationQueue` and the replica is marked deferred (`:913-921`, `flush_deferred_ops` at `:1276-1288`). This is the mechanism that makes concurrent branches safe: an edit is not applied until everything it depends on has been.

`apply_remote_edit` (`text.rs:957-1150`) rewrites the fragment list in one pass over the old tree with a `FragmentBuilder` that grafts untouched `Arc` subtrees and only rebuilds the touched regions (`:976-982`, `:1122-1141`; `FragmentBuilder` at `:2902-2953`, with the comment at `:2931-2934` explaining the spine-grafting). The three anchor-relevant cases:

- **New text** (`:1055-1079`): `push_fragments_for_insertion` (`:1864-1905`) splits the new text into `MAX_INSERTION_LEN` chunks, each becoming a `Fragment` with the edit's `timestamp`, an accumulating `insertion_offset` (`:1891`, `:1901`), a fresh Locator between the running max and the next old fragment's id (`:1889`), `visible: true`, and a corresponding `InsertionFragment` upsert (`:1898`). `RopeBuilder::push_str` appends to the visible rope (`:2997-2999`).
- **Deletion** (`:1083-1119`): each intersecting fragment is cloned into an `intersection` with `len` clipped to the overlap and `insertion_offset` rebased (`:1089-1091`), gets a fresh Locator (`:1092-1093`), and — only if it `was_visible(version, &undo_map)` at the edit's version — is marked `intersection.deletions.push(timestamp); intersection.visible = false;` plus an `InsertionSlice` recorded for undo (`:1094-1099`). The `usize`/`FullOffset` prefix dimensions and the `FragmentTextSummary` partition the moved bytes into `visible` vs `deleted`, and `RopeBuilder::push_fragment` routes the bytes into `new_deleted` (`:1111-1113`, `:2977-2995`).
- **Concurrent higher-Lamport insertions at the same point are skipped over, not deleted** (`:1027-1038`): `if fragment_start == range.start && fragment.timestamp > timestamp { ...keep... }`. This is the CRDT's tie-break: at a tie point, the higher Lamport insertion wins the left position.

The rewritten structures are committed at `:1144-1148` (`fragments`, `visible_text`, `deleted_text`, `insertions.edit(new_insertions, ())`, `insertion_slices.extend(...)`). `SumTree::edit` is an **upsert**: it removes any existing item whose key equals the new key and inserts the new item (`crates/sum_tree/src/sum_tree.rs:1222-1234`). That upsert — keyed by `(timestamp, split_offset)` — is exactly what keeps the anchor→Locator mapping correct as fragments split.

`fragment_ids_for_edits` (`text.rs:1152-1219`) is the reverse index used by undo: given edit ids, look up their `InsertionSlice`s in `insertion_slices` (`:1166-1177`), sort by insertion identity then range (`:1178-1187`), then walk the `insertions` tree to collect the affected `fragment_id`s (`:1191-1218`). `UndoMap::insert` (`crates/text/src/undo_map.rs:51-57`) keys undone edits by `(edit_id, undo_id)`, and `is_undone`/`was_undone` (`:68-70`, `:71-92`) answer "was this edit undone as of version V" by a cursor over that map — `was_undone` takes the max `undo_count` over only those undo entries whose `undo_id` the given version has observed (`:82-89`), so undo parity is itself a versioned query. `apply_undo` (`crates/text/src/text.rs:1221-1274`) then recomputes `fragment.visible = fragment.is_visible(&self.undo_map)` (`:1241`) and rebuilds the ropes.

#### 2.3.4 Versions and clock timestamps

- `clock::Lamport { value: Seq (u32), replica_id: ReplicaId (u16) }` with derived `Ord` = value then replica (`crates/clock/src/clock.rs:62-68`, `:214-221`). `MIN = {0, replica 0}`, `MAX = {u32::MAX, replica u16::MAX}` (`:230-238`). `tick` returns the current value and increments (`:251-255`); `observe` sets `value = max(value, observed) + 1` (`:257-259`).
- `clock::Global` is the version vector: `SmallVec<[u32; 4]>` indexed by replica id (`:50-53`, `:71-73`). `observe` max-merges one timestamp (`:82-93`), `join` is the pointwise max (`:120-127`), `meet` the pointwise min with the zero-means-unobserved convention (`:129-154`). Queries: `observed(ts) = get(replica) >= ts.value` (`:160-162`), `observed_any` (`:164-168`), `observed_all` (`:170-177`).
- `Global::observe` has `debug_assert_ne!(timestamp.replica_id, Lamport::MAX.replica_id)` (`:104`) — you cannot observe the `MAX` sentinel. This is why `can_resolve` special-cases `is_min`/`is_max` before consulting `version` (`text.rs:2671-2674`).
- Base text is materialized as real fragments at `clock::Lamport::new(ReplicaId::LOCAL)` = `{value: 1, replica 0}` (`text.rs:768-803`, `crates/clock/src/clock.rs:240-245`), observed into the version vector at `text.rs:772`. So there is no "untimestamped" text: even the initial file contents have an insertion identity.

**"Resolving an anchor at version V."** There are three distinct things this can mean, and the code base supports them separately:

1. *Anchors are version-free.* An `Anchor` names an insertion identity, so the same anchor is meaningful at every version that observes it. `BufferSnapshot::offset_for_anchor` resolves it against *that snapshot's* version (`text.rs:2509-2543`), and the only version check is the `debug_assert!` at `:2516-2521` plus the post-hoc `panic_bad_anchor` at `:2526`.
2. *Can it be resolved at all?* `can_resolve(&anchor)` (`text.rs:2671-2674`) = `remote_id == anchor.buffer_id && (is_min || is_max || version.observed(anchor.timestamp()))`. This is the non-panicking gate. There is no historical-snapshot retention: the buffer keeps only the newest snapshot plus a version vector, so "resolve at V" for old V means *resolve now, then map back*.
3. *Mapping the current resolution back to V.* `offsets_to_version(offsets, version)` / `range_to_version` (`text.rs:2824-2858`) walk `edits_since(version)` and translate new offsets into old ones, clamping overshoot into the edit (`:2852-2853`). `rope_for_version(version)` (`text.rs:2076-2117`) reconstructs the text as of V by filtering fragments with `!version.observed_all(&summary.max_version)` and swapping each fragment's bytes between the visible and deleted ropes according to `fragment.was_visible(version, &self.undo_map)` (`:2095-2106`). `edits_since(since)` / `anchored_edits_since(since)` (`:2692-2716`) enumerate `Edit<D>` since V, and the "anchored" variants additionally return `Range<Anchor>` for each change (`:2730-2779`, iterator implementation at `:3008-3113`).

`Fragment::was_visible(version, undos)` (`text.rs:3120-3126`) is the core historical query:

```rust
(version.observed(self.timestamp) && !undos.was_undone(self.timestamp, version))
    && self.deletions.iter()
        .all(|d| !version.observed(*d) || undos.was_undone(*d, version))
```

i.e. "the insertion existed at V and was not undone, and every deletion of it had not yet happened at V (or was undone by V)". `Fragment::is_visible` is the same query at the current version (`:3116-3118`).

### 2.4 `crates/rope/src/rope.rs` — only what anchor resolution needs

`Rope` is a `SumTree<Chunk>` (`rope.rs:25-28`), a B+ tree of text chunks. `Rope::cursor(offset)` (`:332`) and `Cursor::summary::<D>(end_offset) -> D` (`:745`) give an **O(log n) prefix summary**: `Cursor::summary` sums chunk summaries between the cursor's current offset and `end_offset`, asserting it never moves backward and never summarizes past the end (`:745-760`).

`TextSummary` (`rope.rs:1282-1304`) is the rich summary: `len` (bytes), `chars`, `len_utf16`, `lines: Point`, `first_line_chars`, `last_line_chars`, `last_line_len_utf16`, `longest_row`, `longest_row_chars`. Its `AddAssign` (`:1404-1433`) is the concatenation algebra that makes subtree summaries composable — including the non-obvious `longest_row` fixups (`:1406-1414`) and first/last line joining (`:1416-1426`).

`TextDimension` (`rope.rs:1441-1447`) is the trait that lets a *caller* choose which quantity to accumulate while walking: `from_text_summary`, `from_chunk`, `add_assign`. `usize` is the byte-length dimension (`:1492-1514`), `Point`/`PointUtf16`/`OffsetUtf16` are others, and `Dimensions<D1, D2, ()>` pairs two of them into one walk (`:1449-1466`). This is why `Anchor::to_offset` and `Anchor::to_point` are the *same* prefix computation with different dimension types: `OffsetRangeExt::to_point` is implemented as `to_offset(..).to_point(snapshot)` (`crates/text/src/anchor.rs:213-225`), and `to_point` on a `usize` rides `visible_text.cursor(offset).summary(...)`.

Anchors themselves never touch the rope directly; they only ever touch `fragments` (for the prefix count) and then hand the resulting `usize` to the rope. The rope is the *value* store; `fragments` is the *index*.

### 2.5 `crates/sum_tree/src/sum_tree.rs` — why resolution is efficient

`SumTree` is *"A B+ tree in which each leaf node contains `Item`s of type `T` and a `Summary`s for each `Item`. Each internal node contains a `Summary` of the items in its subtree. The maximum number of items per node is `TREE_BASE * 2`. Any `Dimension` supported by the `Summary` type can be used to seek to a specific location in the tree."* (`sum_tree.rs:206-213`). `TREE_BASE` is 2 under `cfg(test)` and 6 otherwise (`:15-18`), so a node holds at most 4 items in tests and 12 in production — hence tree height ≈ `log_12 N`.

The vocabulary (`:34-153`): `Item::summary`, `KeyedItem::key`, `Summary::zero/add_summary`, `Dimension<'a, S>` (a *measurable projection* of a summary, `:95-110`), `SeekTarget` (how a query compares against an accumulated dimension, `:122-130`), and `Dimensions<D1, D2, D3 = ()>` (a tuple of dimensions, `:138-153`). `Bias` is documented at `:167-195` with exactly the anchor-relevant example: *"Given the buffer `AˇBCD`: the offset of the cursor is 1; `Bias::Left` would attach the cursor to the character `A`; `Bias::Right` would attach the cursor to the character `B`."*

`SumTree::find` / `find_exact` / `find_with_prev` (`:400-448`, `:511-533`) are the "cursor-free" single-descent lookups: they compare the target against each child's accumulated dimension and descend into the first child that contains it (`find_iterate`, `:450-507`); `find_with_prev` additionally remembers the last item passed (`:564`), which is what `try_find_fragment` needs to answer "which fragment contains this offset". `Cursor::seek`/`seek_forward` (`crates/sum_tree/src/cursor.rs:408-428`) do the same with a persistent stack (`seek_internal`, `:465-...`), and `Cursor::summary(end) -> Output` (`:452-459`) aggregates a dimension over everything between the cursor position and `end` while traversing, which is what makes `offset_for_anchor`'s `start.1` and `Cursor::summary` in the rope O(log n) rather than O(n).

**Complexity of one anchor resolution.** `try_find_fragment` = one `insertions.find_with_prev` (height of the insertions tree, ≤ 12 children per node) → O(log N). `fragments.find::<Dimensions<Option<&Locator>, usize>>` = one descent → O(log N). Adding the overshoot and converting the resulting byte offset to a `Point` = one rope `Cursor::summary` → O(log C) in chunks. Total **O(log N)** per anchor, with `N` = number of fragments; no scan of the text and no dependence on edit distance. `summaries_for_anchors_with_payload` (`text.rs:2442-2500`) turns a *sorted* batch into roughly one amortized forward walk. Anchor `cmp` is O(log N) per comparison because it looks up the fragment id (`anchor.rs:91-103`).

The memory cost of this index is what pays for it: every fragment (including tombstones) stays in the tree, and the deleted bytes stay in `deleted_text` (`text.rs:1141-1148`, `:2061-2068`).

### 2.6 Tests that pin the invariants

**`crates/text/src/locator.rs`**

- `test_locators` (`:114-144`) — pins **`between` really is strictly between, and reuses a limb of one of the inputs.** Asserts `middle > lhs`, `middle < rhs`, and `middle.0[ix] == lhs.0[ix] || middle.0[ix] == rhs.0[ix]` for every non-final limb (`:136-143`), over 100 randomized iterations.
- `test_sequential_forward_append_stays_at_depth_1` (`:146-158`) — comment: *"Simulates 100,000 sequential forward appends (the pattern used when building a buffer's initial fragments and when `push_fragments_for_insertion` chains new text fragments)."* Asserts `loc.len() == 1` every iteration (`:155`). This pins the `>> 48` optimization: pure append never grows the fraction.
- `test_typing_at_cursor_stays_at_depth_2` (`:160-176`) — comment: *"Simulates the most common real editing pattern: a fragment is split (producing a depth-2 prefix), then 10,000 new fragments are inserted sequentially forward within that split region."* Asserts `prefix.len() == 2` (`:167`) and `loc.len() == 2` for 10,000 subsequent insertions (`:173`). Together these pin **bounded Locator depth for the two hot editing patterns** — Locators are variable-length in theory but effectively 1–2 limbs in practice.

**`crates/text/src/text.rs`**

- `Buffer::check_invariants` (`:1742-1787`) — this is an executable invariant checker called throughout the test suite (and by the fuzz tests). The invariants it asserts:
  - *"Ensure every fragment is ordered by locator in the fragment tree and corresponds to an insertion fragment in the insertions tree."* (`:1743-1744`): fragments strictly ascending by `id` (`:1746-1748`), and each fragment has an `insertions` entry at exactly `(fragment.timestamp, fragment.insertion_offset)` pointing back at the same `fragment_id` (`:1750-1765`). **This is the anchor-resolution invariant**: the two indices must agree, or `try_find_fragment` would map an anchor to the wrong place.
  - the reverse direction: walking `insertions` and seeking `fragments` by `fragment_id` must land on a fragment with the same id and `insertion_offset` (`:1768-1774`).
  - `fragment_summary.text.visible == visible_text.len()` and `fragment_summary.text.deleted == deleted_text.len()` (`:1776-1784`) — the `usize`/`FullOffset` dimensions and the two ropes must not drift.
  - the buffer never contains `\r\n` (`:1786`; normalization at `new`, `:750-751`).

**`crates/text/src/tests.rs`**

- `test_anchors` (`:408-523`) — pins the bias semantics, which is the whole reason `Bias` is stored in the anchor:
  - after `buffer.edit([(1..1, "def\n")])` on `"abc"`, both `anchor_before(2)` and `anchor_after(2)` resolve to 6 (`:414-419`);
  - after deleting `2..3`, both resolve to 5 (`:421-426`);
  - after inserting `"ghi\n"` at offset 5, the **left** anchor stays at 5 while the **right** anchor moves to 9 (`:428-433`) — an insertion exactly at the anchor point goes to the right of a left-biased anchor and to the left of a right-biased one;
  - after deleting `7..9`, the left anchor stays 5 and the right anchor becomes 7 (`:435-440`) — a deletion ending exactly at the anchor point does not move it;
  - anchoring by `Point` is equal to anchoring by the corresponding offset for every point (`:442-478`) — `Point`/offset granularity is not part of the identity;
  - `cmp` is a total order consistent with document order (`:480-522`).
- `test_anchors_at_start_and_end` (`:526-546`) — pins the **sticky sentinels**: on an empty buffer `anchor_before(0)` is min and `anchor_after(0)` is max; after appending `"abc"` then prepending `"ghi"`, min still resolves to 0 and max still resolves to 9, while real anchors around the original text resolve to 3 and 6 (`:531-545`).
- `test_concurrent_edits` (`:726-750`) — pins **convergence**: three replicas editing disjoint ranges of `"abcdef"` (`:733-738`), all ops delivered in different orders, all three end at `"a12c34e56"` (`:747-749`).
- `test_edit_partially_intersecting_a_deleted_fragment` (`:752-792`) — regression test whose comment names the mechanism: *"applying a remote edit whose FullOffset range partially overlaps a fragment that was already deleted (observed but not visible) used to leave the fragment unsplit, causing the rope builder to read past the end of the rope"* (`:752-755`). It deletes `2..5`, then applies a synthetic remote edit over `FullOffset(1)..FullOffset(4)` *"so the 'cde' fragment is observed + deleted → !was_visible"* (`:766-770`), and asserts the result plus undo (`:788-791`). This pins **`FullOffset` space and the `was_visible` guard as the correctness boundary for concurrent edits over tombstones**.
- `test_random_concurrent_edits` (`:794-872`) — a randomized multi-replica fuzz loop that interleaves local edits, undo/redo, and out-of-order network delivery, calls `buffer.check_invariants()` on **every iteration for every replica** (`:855`, `:870`) and finally asserts all replicas have identical text (`:862-871`). This is the broadest invariant harness: it is where "fragments ordered by locator ⇔ insertions agree ⇔ ropes agree" is enforced under adversarial concurrency.
- Anchor round-trip tests for fragmentation and multibyte text:
  - `test_new_normalized_splits_large_base_text` (`:874-911`) — with a 16-byte `MAX_INSERTION_LEN`, 100 bytes of base text become many fragments; asserts `anchor_before/after(offset).to_offset == offset` for offsets crossing chunk boundaries, including 15/16/17 (`:888-902`).
  - `test_new_normalized_splits_large_base_text_with_multibyte_chars` (`:913-940`) — same, but `"ééééééééé".repeat(6)`, asserting round-trip at *every* char boundary (*"Every anchor should resolve correctly even though chunks had to be rounded down to a char boundary"*, `:929-939`). Pins that **fragment splits never land mid-character**.
  - `test_new_normalized_small_text_unchanged` (`:942-957`) asserts exactly one fragment for small text (`:956`).
  - `test_edit_splits_large_insertion` (`:959-981`) asserts anchor round-trip at offsets `[0, 3, 50, 103, len]` across a 100-byte insertion that had to be split (`:972-980`) — the anchor survives being split into multiple `(timestamp, split_offset)` fragments.
  - `test_edit_splits_large_insertion_with_multibyte_chars` (`:983-996`), `..._among_multiple_edits` (`:998-1020`), `test_edit_splits_multiple_large_insertions` (`:1022-1038`) — the same for 4-byte emoji and for a split interleaved with other edits.
  - `test_edit_undo_after_split` (`:1040-1057`) — *"Undo should restore the original text even though the edit was split into multiple internal operations grouped in one transaction."* Pins that one logical edit maps to many fragments and undo must reverse all of them.

**`crates/sum_tree/src/sum_tree.rs`** and **`crates/rope/src/rope.rs`**: the property-ish tests are `test_random` (`sum_tree.rs:1416-...`, randomized splices checked against a `Vec` model), `test_cursor` (`:1593`), `test_edit` (`:1782`), `test_from_iter` (`:1803`), and on the rope side `test_random_rope` (`rope.rs:1888`), `test_chunks_equals_str` (`:2205`), `test_is_char_boundary` (`:2326`), `test_floor_char_boundary` (`:2345`), `test_ceil_char_boundary` (`:2366`), `test_push_front_random` (`:2419`). Their invariant is the standard one for a summary tree: **the incremental summary/seek machinery must agree with a naive linear model**, which is the precondition for anchor resolution being a pure O(log n) lookup rather than an approximation.

---

## 3. Essential vs. incidental: the minimal data a reference must store

Reading the mechanism backwards, here is what actually does work, versus what is machinery. Format: **essential** = if you remove it, relocation after arbitrary edits breaks; **incidental** = performance, ergonomics, or concurrency-only.

### 3.1 Essential

1. **An identity for the insertion event, independent of content and of current position.** `(replica_id: u16, seq: u32)` (`anchor.rs:18-19`). This survives deletion, reordering by unrelated edits, and fragment splitting. Without it, a reference degrades to an offset, which is destroyed by any edit before it.
2. **A byte offset *inside that insertion*, not inside the document.** `offset: u32` (`anchor.rs:23`) with the doc comment "The byte offset into the text inserted in the operation at `timestamp`". This is the second half of the stable coordinate system: `(which insertion, how far into it)`. It is what makes the reference invariant under splits, because splits only subdivide the insertion string.
3. **A bias bit** (`anchor.rs:26`). Without it, "the boundary between A and B" is ambiguous: is it before or after text inserted at exactly that boundary? The bias is also what makes an anchor at document start/end sticky (`text.rs:2635-2640`).
4. **A buffer/lineage discriminator** (`buffer_id: BufferId`, `anchor.rs:27`). This is what turns "anchor whose content was deleted" from a *dangling pointer into unrelated text* into an *explicitly invalid reference* (`anchor.rs:151-152`, `text.rs:2672`). Commit `49469000f0` exists because this check was once evaluated *after* the min/max short-circuit, letting a min anchor from another buffer validate.
5. **A reverse index `(identity, offset) → current location`** (the `insertions` tree, `text.rs:117`, `:2584-2608`). This is the part that must be *maintained by every edit*; it is the actual CRDT bookkeeping.
6. **A prefix-sum index from current location → current visible offset, that also retains deleted material** (the `fragments` tree + `FragmentTextSummary`, `text.rs:116`, `:588-592`, `:3272-3300`). Without the `deleted` half of the summary you could not tell "before the deletion" from "after it", and `FullOffset`-space edits (`:3282-3290`) would be impossible.
7. **A tombstone flag plus the set of deleting edits** (`Fragment.visible`, `Fragment.deletions`, `text.rs:574-575`). This is what makes deletion non-destructive, which is what lets a reference survive deletion at all. It also makes undo possible (`is_visible`/`was_visible`, `:3116-3126`).

### 3.2 Incidental

- **`Locator` as the join key** (`locator.rs:12`). It exists because the implementation chose *two separate ordered trees* and therefore needs a comparable key that can be spliced into document order without renumbering. A simpler design that keeps one ordered structure with both orderings as dimensions, or that linearly scans a `Vec<Fragment>`, needs no `Locator` at all. Its only essential role is *ordering*, not identification — note that it is never serialized.
- **`SumTree` / rope.** Pure acceleration: O(log n) instead of O(n). A `Vec<(FragmentIdentifier, len, flags)>` with linear scan implements exactly the same semantics.
- **`clock::Global` version vectors and the `max_version` / `min_insertion_version` / `max_insertion_version` summary fields** (`text.rs:582-585`, `clock.rs:50-53`). Essential for *concurrent* editing (deciding whether an op is applicable, computing `edits_since`, reconstructing a past version) but not for single-writer relocation. A single-user CLI needs none of it.
- **`FullOffset` vs visible-offset dual coordinate systems** (`text.rs:3246-3290`). This exists so remote edits can name locations in material that the receiving replica considers deleted. Single-writer relocation needs only visible offsets.
- **`u32` narrowing, inline Lamport packing, `opaque_id`, min/max sentinels.** `anchor.rs:14-17` (padding), `text.rs:51-55` (u32), `anchor.rs:190-200` (UI element id), `anchor.rs:59-89` (`min`/`max` as conveniences for whole-buffer and open-ended ranges). All are size/ergonomics optimizations.
- **`Patch`** (`patch.rs:7-8`). Delivery format for consumers, not part of anchor correctness.
- **`OperationQueue`** (`operation_queue.rs:13`). Out-of-order buffering for collaborative delivery only.
- **Arguable:** `bias` could in principle be folded into `offset` (e.g. offsets counted with a half-step), but the min/max sentinel design and the equality semantics of `cmp` make a separate bit clearer — this is my inference, not something the code states.

### 3.3 The minimal portable tuple

Stripping to the bone, a reference is:

```
(replica_id, seq, offset_in_insertion, bias)         // 4 fields, ~11 bytes packed
+ buffer/lineage id                                  // invalidate across replacements
```

plus a maintained map `(replica_id, seq) -> current document order key` and a maintained `document order key -> visible prefix length` (with tombstones retained). Note that field 4 (`buffer_id`) is not needed to *relocate*; it is needed to *refuse* to relocate. The four-tuple plus two maps is the whole design; rope, sum-tree, version vectors, `Locator`, and `Patch` are all either acceleration or collaboration.

---

## 4. "Content moved" vs "content deleted"

**Short answer: within `crates/text`, there is no move primitive, so the two are not distinguished as events; they are distinguished as *states*. Deletion is represented; relocation is not.**

The mechanism, quoted:

**(a) There is no move operation.** `Operation` has exactly two variants:

`crates/text/src/text.rs:618-622`
```rust
pub enum Operation {
    Edit(EditOperation),
    Undo(UndoOperation),
}
```

**(b) Deletion is a flag plus a record of the deleting edits, on a fragment that is retained:**

`crates/text/src/text.rs:574-575`
```rust
visible: bool,
deletions: SmallVec<[clock::Lamport; 2]>,
```

Applied at `crates/text/src/text.rs:1094-1096` (remote path) and `:2017-2018` (local path):
```rust
if fragment.was_visible(version, &self.undo_map) {
    intersection.deletions.push(timestamp);
    intersection.visible = false;
```
with the bytes re-routed rather than dropped: `RopeBuilder::push_fragment` sends a non-visible fragment into `new_deleted` (`crates/text/src/text.rs:2977-2995`), and the snapshot keeps both ropes (`:1145-1146`, `:2066-2067`). `check_invariants` asserts `fragment_summary.text.deleted == deleted_text.len()` (`:1781-1784`).

**(c) The visibility predicate is a pure query over retained state, so it can be answered at an arbitrary version:**

`crates/text/src/text.rs:3120-3126` (`was_visible`) and `:3116-3118` (`is_visible`), the latter being `!undos.is_undone(self.timestamp) && self.deletions.iter().all(|d| undos.is_undone(*d))`.

**(d) A reference into deleted material resolves to where the deleted run began, and reports invalidity:**

`crates/text/src/text.rs:2537-2540`
```rust
let mut fragment_offset = start.1;
if fragment.visible {
    fragment_offset += (anchor.offset - insertion.split_offset) as usize;
}
```
If the fragment is not visible, the overshoot is not added, so the anchor collapses to the visible offset preceding the tombstone. The separate validity query is `Anchor::is_valid`:

`crates/text/src/anchor.rs:150-168`
```rust
pub fn is_valid(&self, buffer: &BufferSnapshot) -> bool {
    if self.buffer_id != buffer.remote_id { false }
    else if self.is_min() || self.is_max() { true }
    else {
        let Some(fragment_id) = buffer.try_fragment_id_for_anchor(self) else { return false; };
        let (.., item) = buffer.fragments.find::<Dimensions<Option<&Locator>, usize>, _>(
            &None, &Some(fragment_id), Bias::Left);
        item.is_some_and(|fragment| fragment.visible)
    }
}
```

**(e) Relocation is expressed only as a *pair* of edits.** `anchored_edits_since_in_range` (`crates/text/src/text.rs:2730-2779`) returns `(Edit<D>, Range<Anchor>)` per change, and the `Edits` iterator builds the anchor range from the affected fragment's own identity:

- insertion (`!was_visible && visible`): `crates/text/src/text.rs:3049-3073`, with `start_anchor`/`end_anchor` built at `:3036-3047` from `fragment.timestamp` and `fragment.insertion_offset` (with `Bias::Right`/`Bias::Left` respectively, so the range encloses the inserted text);
- deletion (`was_visible && !visible`): `crates/text/src/text.rs:3076-3106`, using the same fragment's anchors.

So a consumer that wants to follow content across an edit does so by consuming an `Edit` in offset space *and* the corresponding `Range<Anchor>`; it never gets told "this moved".

**What the design can therefore say, and what it cannot:**

- It *can* say: "the material this reference names is currently tombstoned" (`is_valid == false`, `anchor.rs:150-168`), "it was visible at version V, deleted at version D" (`was_visible`, `text.rs:3120-3126`, with `D ∈ fragment.deletions`), and "it was deleted and then restored by undo" (`UndoMap::is_undone`, `crates/text/src/undo_map.rs:59-70`, driving `apply_undo` at `text.rs:1241-1256` to flip `visible` back on).
- It *cannot* say: "the material at this reference is now over there", because a cut-and-paste is delete-at-source + insert-at-destination, and the destination's `Fragment` carries a **new** Lamport timestamp minted by `Buffer::edit` at `text.rs:882` and written into the new fragment at `text.rs:1890`. The old anchor names only the source insertion, so after a cut-and-paste it resolves to the tombstone (offset of the cut) with `is_valid == false`, and it will never follow the paste. *This last sentence is inference from the absence of a move variant plus the timestamp minting site; I found no test that exercises cut-and-paste + anchor following, and none could pass.*

Edge case worth stating explicitly (inference from the code path, no test covers it): because `offset_for_anchor` drops the overshoot for invisible fragments (`text.rs:2538-2540`), **all anchors inside one contiguous deleted run collapse to the same visible offset** — the position where the run started — and remain distinct only in `cmp`-order and in their `is_valid`-ness (both false). The `AnchorRangeExt::overlaps` predicate (`anchor.rs:242-244`) still distinguishes them in anchor space.

---

## 5. Degradation and failure modes

Ordered from benign to fatal.

1. **Anchor whose content was deleted — resolvable, invalid.**
   `offset_for_anchor` returns the visible offset at the start of the tombstone run (`text.rs:2537-2540`); `is_valid` returns `false` (`anchor.rs:150-168`); `can_resolve` still returns `true` (`text.rs:2671-2674`, it only checks version observation). No panic, no dangling pointer. Consumers that must not act on dead references call `is_valid` — e.g. bookmarks (`crates/project/src/bookmark_store.rs:406`, `:484`) and diagnostics (`crates/language/src/buffer.rs:3226-3231`, which uses `can_resolve`, i.e. a weaker check).

2. **Anchor restored by undo — invalidity is not permanent.**
   `apply_undo` sets `fragment.visible = fragment.is_visible(&self.undo_map)` (`text.rs:1241`), and `is_visible` consults `UndoMap` (`:3116-3118`). So a reference that reported invalid at version V can report valid at V+1 after an undo. Any "anchor validity" cache must be invalidated on undo, not only on edit.

3. **Anchor from an unobserved (concurrent or future) edit — panic unless guarded.**
   `offset_for_anchor` `debug_assert!`s `version.observed(anchor.timestamp())` (`text.rs:2516-2521`), then `panic_bad_anchor` panics with *"invalid anchor - snapshot has not observed lamport ..."* (`:2552-2556`) when the insertion cannot be found with a matching timestamp (`:2522-2527`). Unmatched anchors panic from `fragment_id_for_anchor` (`:2565-2568`) and from `summaries_for_anchors_with_payload` (`:2464-2471`, plus the `assert_eq!(insertion.timestamp, anchor.timestamp(), ...)` at `:2474-2488`).
   The supported ways to handle this are `can_resolve` (`text.rs:2671-2674`) and `wait_for_anchors` (`text.rs:1575-1599`), which registers a oneshot per unobserved anchor timestamp and fires from `resolve_edit` (`:1626-...`) when the buffer catches up, failing with *"gave up waiting for anchors"* if the buffer is dropped. Remote ops themselves are parked rather than applied early (`apply_ops`/`can_apply_op`/`flush_deferred_ops`, `:909-922`, `:1290-1299`, `:1276-1288`).

4. **Anchor from a different buffer or a replaced buffer — rejected, and this was a real crash.**
   `buffer_id` mismatch → `is_valid == false` (`anchor.rs:151-152`) and `can_resolve == false` (`text.rs:2672`). Commit `49469000f0` is exactly the fix: the commit message says it was *"possible for two anchors to satisfy `a1 < a2` but `a1.to_offset() > a2.to_offset()`"* and that *"Changes of diff base and normal buffer edits could cause two anchors to sort `a1 < a2` against one snapshot and `a2 < a1` against a later snapshot"*, causing crashes in code that sorted anchors by `cmp`. The diff moved the `buffer_id` check *ahead of* the min/max short-circuit in both `Anchor::is_valid` and `BufferSnapshot::can_resolve` (the pre-fix bodies returned `true`/`can_resolve == true` for min/max anchors before checking the buffer id).

5. **Ordering an unresolvable anchor panics.**
   `Anchor::cmp` calls `buffer.fragment_id_for_anchor(self)` (`anchor.rs:96-97`) which is `try_fragment_id_for_anchor(...).unwrap_or_else(|| self.panic_bad_anchor(anchor))` (`text.rs:2565-2568`). Therefore the *derived* orderings `min`/`max`/`overlaps`/`contains_anchor` (`anchor.rs:105-119`, `:228-248`) and `Range<Anchor>::cmp` can all panic on a stale or foreign anchor. Sorting a mixed collection of anchors requires filtering by `can_resolve`/`is_valid` first.

6. **Release-mode silent misresolution (inference).**
   `try_fragment_id_for_anchor` only verifies that the found insertion's timestamp matches the anchor's under debug assertions:
   `crates/text/src/text.rs:2577-2579`
   ```rust
   item.filter(|insertion| {
       !cfg!(debug_assertions) || insertion.timestamp == anchor.timestamp()
   })
   ```
   In release the filter is a no-op, so an anchor whose timestamp is absent can silently resolve to a *neighbouring* fragment's position instead of panicking. Similarly `offset_for_anchor`'s two `debug_assert!`s (`:2515-2521`) become no-ops. The resulting offset is syntactically valid but semantically wrong. I found no test for this path.

7. **Sentinel anchors are never invalid, so they can be "safely wrong".**
   `is_valid` short-circuits `true` for min/max (`anchor.rs:153-154`) and `offset_for_anchor` returns `0` / `len` (`text.rs:2510-2513`). If a caller expected a real position and the anchor was constructed at offset 0 with left bias or at end with right bias, it silently gets buffer start/end. This is by design for open-ended ranges (`anchor_range_inside`/`outside`, `:2610-2618`) but it means validity checks cannot detect sentinel misuse.

8. **Buffer replacement / rebasing invalidates wholesale.**
   Because `buffer_id` is the discriminator (and is `NonZeroU64`, `text.rs:70-72`), a new buffer incarnation (new id) invalidates every anchor from the old one rather than silently re-homing it. That is the intended degradation for diff-base swaps and is the failure mode the `49469000f0` commit tested downstream.

9. **No version pruning exists, so anchors never expire from GC — but there is also no bound on memory.**
   The `fragments` tree and `deleted_text` rope only grow (`apply_edit_internal` rewrites but never drops: `text.rs:2064-2068`; `apply_remote_edit` likewise `:1144-1148`), and the only `truncate` in the crate is on `undo_stack` (`:335`). `History.operations` is insert-only (`:234-236`; the only other mention is a read at `:392`). Consequence: *anchor resolution cannot fail due to pruned history*, because nothing is pruned. If pruning were ever added, `try_find_fragment` would return `None` or the wrong piece and `offset_for_anchor` would hit `panic_bad_anchor` (`:2526`) — there is no "expired reference" state distinct from "corrupt reference".

10. **Line-ending normalization shifts offsets.**
    `Buffer::new` calls `LineEnding::normalize(&mut base_text)` (`text.rs:750-751`) and edits normalize incoming text with `LineEnding::normalize_arc` (`:1947`). Anchors are offsets into the *normalized* text, so an offset computed from the on-disk file's bytes will not line up with an anchor unless the same normalization is applied. `check_invariants` pins `!self.text().contains("\r\n")` (`:1786`).

---

## 6. History-retention requirements

**Does anchor resolution need the full CRDT operation history?**

**No — it does not consult the operation log at all.** The log is `History.operations: TreeMap<Lamport, Operation>` (`text.rs:153-160`, written only at `:234-236`). Anchor resolution touches exactly three things:

- `self.insertions` (index by `(timestamp, split_offset)`, `text.rs:2584-2608`),
- `self.fragments` (index by `Locator` plus prefix dimensions, `:2529-2537`),
- `self.version` (the version vector, `:2516-2521`, `:2671-2674`).

It never calls `buffer.operations()` or `history.operations`. `History` is not even reachable from `BufferSnapshot` (`text.rs:112-124` has no history field; history lives on `Buffer`, `:59-68`).

**Direct evidence that the op log is disposable while anchors remain usable:** `Buffer::branch` (`text.rs:841-852`) clones the snapshot — hence `fragments`, `insertions`, `version`, `deleted_text` — but resets history:

```rust
pub fn branch(&self) -> Self {
    Self {
        snapshot: self.snapshot.clone(),
        history: History::new(self.base_text().clone()),   // operations log discarded
        ...
        lamport_clock: clock::Lamport::new(ReplicaId::LOCAL_BRANCH),
        ...
    }
}
```

So a branch has all the anchor-resolution state of its parent and none of its operation log. Anchors created in the parent resolve in the branch. Undo across the branch point is, correspondingly, not available (empty stacks).

**What *is* required to be retained: the fragment/insertion index and the deleted bytes.** Specifically:

- every `Fragment` that any live anchor might name must still be in `fragments` — including `visible == false` ones, since resolution queries them (`text.rs:2529-2541`) and `is_valid` inspects `fragment.visible` (`anchor.rs:159-167`);
- every `InsertionFragment` keyed by `(timestamp, split_offset)` must be present for the fragment containing the anchor offset, because the `fragments` lookup is keyed by the `Locator` it returns (`text.rs:2584-2608`, `:2529-2535`);
- the `deleted_text` rope must contain the bytes of invisible fragments if you want version reconstruction (`rope_for_version`, `text.rs:2095-2103`) or `Edit` generation in `FullOffset`/deleted space (`:3077-3089`);
- the version vector must be retained to answer `observed` (`clock.rs:160-162`) — but this is O(#replicas), not O(#operations).

Because the index is never pruned, there is currently no bound at all: memory grows with cumulative insertions *and* with cumulative deletions.

**Can it work with only a bounded window of operations?**

**Not as implemented.** The API offers no window and no "expired" error: a missing fragment is indistinguishable from a corrupt anchor, and both panic (`text.rs:2526`, `:2546-2563`) or, in release, silently misresolve (§5.6). To support a bounded window you would have to add one of:

- an explicit tombstone/expiry state on `Anchor` (so a dropped fragment means "expired", not "invalid"), or
- a fallback locator that does not depend on retained identity (content hash + context — see §7), consulted when `try_find_fragment` misses.

What *is* already windowed: the deferred-operation buffer (`OperationQueue`, `text.rs:62`, `operation_queue.rs:13-64`) and the undo/redo stacks (`History.undo_stack`/`redo_stack`, `text.rs:156-157`, with `forget_transaction` at `:1386-1389` removing entries from the undo stack only). Neither of these affects anchor resolvability: `undo_map` (§2.3.1) does, and it also only grows (`UndoMap::insert` at `crates/text/src/undo_map.rs:45-58`).

**Inference about DeltaDB.** A version-control layer wants to ask "what did this region look like at commit X" over a long history. The `text` layer's answer is `was_visible(version, undos)` (`text.rs:3120-3126`) plus `rope_for_version` (`:2076-2117`) — which requires retaining all fragments *and* a version vector per query point. A version-control system that wants bounded storage would need exactly the tombstone-expiry or content-hash fallback described above. I cannot verify how (or whether) DeltaDB does this, because DeltaDB is not in this checkout (§0).

---

## 7. Simplified portable design: relocation without a rope or a full CRDT

Goal: a self-contained relocation primitive for a Go/TypeScript/Rust/Zig CLI that can be stored in a JSON file, that survives arbitrary edits, and that does not require a B+ tree, a per-character CRDT, or the full operation log. Below is a design that keeps every *essential* property identified in §3 while dropping all of the incidental machinery, plus an explicit statement of what you lose.

### 7.1 The reference tuple (what to persist)

```jsonc
{
  "path": "src/main.rs",          // file identity (cheap stand-in for buffer_id)
  "blob": "sha256:9f2c...",       // hash of the file content WHEN the ref was made  (lineage guard)
  "anchor_offset": 4213,          // byte offset into the *anchor window* below
  "bias": "right",                // "left" | "right"
  "before": ["fn main() {", "    let x = 1;"],   // up to N context lines before
  "after":  ["    println!(\"{}\", x);"],        // up to N context lines after
  "before_hash": "sha256:...",    // hashes of those exact lines (content identity)
  "after_hash":  "sha256:...",
  "ordinal": 2,                   // which occurrence of this window, if duplicated
  "checkpoint": "git:abc123"      // optional: commit the offsets were computed at
}
```

The three load-bearing pieces map one-to-one onto Zed's essentials:

| Zed essential (§3.1) | Portable equivalent |
|---|---|
| insertion identity `(replica_id, seq)` | the `(before_hash, after_hash, ordinal)` **content fingerprint** |
| offset inside the insertion | `anchor_offset` inside the matched window |
| `bias` | `bias` |
| `buffer_id` lineage | `path` + `blob` + optional `checkpoint` |

If you *do* keep an append-only edit journal (a text file of `(seq, offset, deleted_len, inserted_len, inserted_hash)`, i.e. a poor man's `EditOperation` in `FullOffset` space), use `(session_uuid, seq)` for identity exactly like Zed does and keep `anchor_offset` relative to *that insertion* rather than to a window. That is strictly stronger — see §7.3.

### 7.2 The algorithm

**Create** (`anchor_new`, mirrors `BufferSnapshot::anchor_at_offset`, `text.rs:2634-2669`):

1. Round the requested offset to a UTF-8 char boundary in the bias direction (Zed: `text.rs:2642-2650`). Reject or clamp out-of-range.
2. Capture `before` = the K lines immediately preceding the offset (K = 3 is a good default; git's default diff context is 3) and `after` = the K lines following it. Never include the offset's own line if you want the anchor to survive edits *on* that line.
3. Store `anchor_offset` = byte distance from the start of `before` to the requested offset, and `bias`. Because the window is content-addressed, this offset is meaningful even after the surrounding file changes.
4. Record `blob` = hash of the whole file at creation, and `ordinal` = how many earlier occurrences of the same `(before, after)` pair exist in this file (0-based).

**Resolve** (`anchor_resolve`, mirrors `try_find_fragment` + `fragments.find`):

1. If `path` no longer exists or `blob`/`checkpoint` indicates the caller wants strict lineage, return `Invalid{reason: "lineage"}` — this is the `buffer_id` check (`anchor.rs:151-152`, `text.rs:2672`) and it is what prevents landing in unrelated content.
2. Compute line hashes of the new file once (O(file size), one pass) and build a hash-map from each line hash to its line indices. This is the entire "index"; there is no tree.
3. Find candidate positions where the `before_hash` tail matches immediately followed by the `after_hash` head. Concretely: for each line index `i` whose line hash equals the last element of `before_hash`, check backwards that the preceding lines match `before_hash`; for each line index `j` whose line hash equals the first element of `after_hash`, check forwards. A matching pair `(i, j)` with `j == i+1` and matching `anchor_offset` modulo window length is a hit. (Zed's equivalent is `try_find_fragment` locating the insertion whose key contains `(timestamp, offset)`, `text.rs:2584-2608`.)
4. If there are multiple hits, disambiguate with `ordinal`; if the count is unchanged, take `min(ordinal, hits-1)`; if it changed, prefer the hit nearest the reference's original absolute line number (store it as a fallback field) or return `Ambiguous{candidates}`. Zed has no analogue here because `(timestamp, offset)` is unique by construction — this is the price of content addressing.
5. If there are zero hits, degrade in this order (each step is *reported*, never silent):
   a. drop one context line from `before` and one from `after` (widen the search) and retry;
   b. match on `before` alone (or `after` alone) and return `Degraded` with the residual uncertainty;
   c. if the window's own text can be hashed and located anywhere in the file, return `Moved{new_offset}` — this is the key capability Zed's design *lacks* (§4);
   d. otherwise return `NotFound` (this is Zed's `is_valid == false` / `panic_bad_anchor`, except that a CLI should return a value, not panic — see §5.5).
6. For a range, store two references with `bias = "outside"` semantics (start = right-biased, end = left-biased) so the range grows with insertions at its edges, exactly like `anchor_range_inside` (`text.rs:2611-2613`). This is a one-line policy decision, not a data-model change: the bias bit carries it.

**Ordering two references without resolving them.** This is the one thing the content-addressed design cannot do as cheaply as Zed. Zed orders anchors by fragment id + offset + bias (`anchor.rs:91-103`) in O(log n) with no text access. Portably, either (a) resolve both to offsets and order those (O(file) each, cache the line-hash table across the operation), or (b) store a monotonically increasing `order_key` alongside the reference, regenerated on write — which is Zed's `Locator` idea (`locator.rs:4-12`) reduced to "renumber occasionally instead of fractionally". Option (a) is fine for a CLI that resolves a handful of references per run; option (b) is the correct choice if you sort thousands.

### 7.3 What you keep, and what you give up

**Kept, by construction:**

- Survival of arbitrary edits *before*, *after*, and *inside* the anchor's context lines, including fragment-splitting analogues (line boundaries are the fragmentation).
- Bias semantics identical to Zed's, including the sticky-start/end behaviour if you special-case offset 0 / EOF as sentinels.
- Explicit invalid/ambiguous/degraded outcomes instead of panics.
- **The ability to detect and follow a move**, which Zed's `text` CRDT cannot do (§4): because the fingerprint is content, the same window found at a different offset *is* a move, and you can report `Moved{from, to}`.

**Given up, relative to Zed:**

- **Uniqueness.** Zed's `(replica_id, seq, offset)` names exactly one position even in a file of identical lines. Content fingerprints are ambiguous under duplication, and no amount of context fully removes that in pathological files (repeated boilerplate, minified code, generated files). This is inherent; git, `diff-match-patch`, and `patch -l` all share it.
- **O(log n) resolution.** A line-hash pass is O(file size) per resolve, or O(1) amortized if you cache the table for the duration of a command. Zed is O(log n) *per anchor* against a persistent index. For a CLI processing one file per invocation, O(file) is usually the right trade; for a long-running server doing thousands of resolutions per keystroke, it is not, and you would want to keep an index and re-index on save.
- **True tombstones.** A CLI cannot afford to retain deleted text forever, so "deleted" is `NotFound`/`Degraded`, not "resolvable but `is_valid == false`". If you need Zed's distinction (§4) you must keep the deleted bytes, which means keeping the file's old blob (and you already have `blob`, so you *can* recover the deleted text from git/the old blob if the caller asks).
- **Concurrency.** No version vectors, no deferred ops, no `wait_for_anchors` analogue. If you need collaborative editing, you need a real CRDT; a content-addressed reference is a *reader-side* locator, not a merge primitive.

**A middle path that keeps most of both.** Persist an append-only journal of edits per file (`{seq, full_offset_start, deleted_len, inserted_len}`) — this is Zed's `EditOperation` minus the version vector — and store references as `(seq, offset_into_that_insertion, bias)`. Replay the journal from the reference's checkpoint to the current text with a single forward merge (exactly `apply_edit_internal`'s loop, `text.rs:1946-2043`, minus the CRDT/version machinery) to compute the new offset. This gives Zed's exactness and O(1)-per-edit relocation with no tree, no rope, and no tombstones kept in memory; the cost is that references become invalid once the journal is truncated, so you must either never truncate (Zed's current choice) or fall back to the fingerprint scheme of §7.1 for references older than the window. Combined: **journal for recent/strong references, content fingerprint for old/weak ones, one `resolve` entry point that tries the journal first and falls back with an explicit `Degraded` result.**

---

## Appendix A — citation index for the main claims

| Claim | Citation |
|---|---|
| Anchor is `(timestamp, offset-in-insertion, bias, buffer_id)` | `crates/text/src/anchor.rs:11-28` |
| Inline Lamport packing saves 8 bytes | `crates/text/src/anchor.rs:14-17` |
| Anchor creation, min/max sentinels, char-boundary rounding | `crates/text/src/text.rs:2630-2669`; `crates/text/src/anchor.rs:59-89` |
| Anchor resolution algorithm | `crates/text/src/text.rs:2509-2543`; `:2584-2608` |
| Panic on unresolvable anchor | `crates/text/src/text.rs:2526`, `:2545-2563` |
| `is_valid` = fragment exists and is visible | `crates/text/src/anchor.rs:150-168` |
| `can_resolve` = buffer id + version observed | `crates/text/src/text.rs:2671-2674` |
| Anchor ordering by fragment id | `crates/text/src/anchor.rs:91-103` |
| Buffer-id check ordering fixed | exec. `49469000f0` diff of `crates/text/src/anchor.rs:148-158`, `crates/text/src/text.rs:2669-2674` |
| `Locator` = fraction between neighbours | `crates/text/src/locator.rs:4-12`, `:52-65` |
| `Locator` depth tests | `crates/text/src/locator.rs:114-176` |
| `Locator` reassigned on fragment rewrite | `crates/text/src/text.rs:1047`, `:1093`, `:1889`, `:1976`, `:2016` |
| `Locator` never serialized | no serde/proto in `crates/text/src/locator.rs`; `crates/proto/proto/buffer.proto:238-244` covers `Anchor` only |
| Insertion index `(timestamp, split_offset) → Locator` | `crates/text/src/text.rs:605-616`, `:1864-1905`, `:3200-3217` |
| Upsert semantics of the index | `crates/sum_tree/src/sum_tree.rs:1222-1234`; commit site `crates/text/src/text.rs:1147` |
| Delete = tombstone + deletions list + bytes moved to `deleted_text` | `crates/text/src/text.rs:574-575`, `:1094-1099`, `:2011-2032`, `:2977-2995` |
| `is_visible` / `was_visible` | `crates/text/src/text.rs:3116-3126` |
| `Operation` has no move variant | `crates/text/src/text.rs:618-622` |
| New insertion gets a fresh Lamport | `crates/text/src/text.rs:882`, `:1890` |
| `EditOperation` fields; `version` is pre-edit | `crates/text/src/text.rs:624-630`, `:1924` |
| `FullOffset` = visible + deleted | `crates/text/src/text.rs:3246-3290`, `:1970` |
| Deferred remote ops until causally ready | `crates/text/src/text.rs:909-922`, `:1276-1299` |
| `wait_for_anchors` | `crates/text/src/text.rs:1575-1599` |
| Branch discards op log but keeps snapshot | `crates/text/src/text.rs:841-852` |
| `History.operations` insert-only | `crates/text/src/text.rs:153-160`, `:234-236` |
| No pruning; only `undo_stack.truncate` | `crates/text/src/text.rs:335` |
| Invariant checker | `crates/text/src/text.rs:1742-1787` |
| Anchor behaviour tests | `crates/text/src/tests.rs:408-546` |
| Concurrency + anchor-round-trip fuzz | `crates/text/src/tests.rs:726-750`, `:794-872`, `:874-1020` |
| `SumTree` is a B+ tree with 12 items/node | `crates/sum_tree/src/sum_tree.rs:15-18`, `:206-213` |
| `find` / `find_with_prev` single descent | `crates/sum_tree/src/sum_tree.rs:426-448`, `:511-533`, `:450-507` |
| Dimension / SeekTarget / Dimensions | `crates/sum_tree/src/sum_tree.rs:95-153` |
| `Bias` semantics with worked example | `crates/sum_tree/src/sum_tree.rs:167-195` |
| Rope is a `SumTree<Chunk>`; `Cursor::summary` | `crates/rope/src/rope.rs:25-28`, `:745-760` |
| `TextSummary` contents and concatenation algebra | `crates/rope/src/rope.rs:1282-1304`, `:1404-1433` |
| `TextDimension` extension trait | `crates/rope/src/rope.rs:1441-1466`, `:1492-1514` |
| Anchor wire format | `crates/proto/proto/buffer.proto:238-244`, `crates/language/src/proto.rs:265-278`, `:529-550` |
| DeltaDB mentioned only in a comment | `crates/collab/tests/integration/random_project_collaboration_tests.rs:1276` |

## Appendix B — explicitly labelled inferences

These are conclusions I drew from reading the code but did not see stated or tested in it:

1. **A cut-and-paste does not move an anchor.** Follows from the absence of a move `Operation` variant (`text.rs:618-622`) and the fresh timestamp minted per insertion (`text.rs:882`, `:1890`). No test covers it.
2. **All anchors inside one contiguous deleted run collapse to the same visible offset.** Follows from the conditional overshoot at `text.rs:2538-2540`. No test covers it.
3. **Release builds can silently misresolve unknown anchors.** Follows from the `!cfg!(debug_assertions) ||` guard at `text.rs:2577-2579` and the `debug_assert!`s at `:2515-2521`. No test covers release behaviour.
4. **`bias` could be folded into `offset`.** Design opinion; the code does not discuss it.
5. **DeltaDB likely needs a tombstone-expiry or content-hash fallback to bound storage.** Inference about a component not present in this checkout; stated as a hypothesis in §6.
6. **The line-hash-table resolution cost is O(file) per resolve** and therefore unsuitable for per-keystroke server-side use. Arithmetic on the design in §7, not a measurement.
