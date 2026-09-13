# DeltaDB — Architecture and Portable Primitives

**Research report.** Subject: DeltaDB, the operation-level version control system announced by Zed Industries (makers of the Zed editor), and its first client, **Delta**.

**Access constraint:** DeltaDB/Delta was in private/closed beta at the time of research. Everything below is from public sources only: Zed's own blog, the `delta.dev` product documentation, the Zed roadmap, one podcast interview with Zed's CEO, GitHub discussions, press coverage, and Hacker News threads. No product binary, schema, API spec, or source code was inspected.

**Evidence discipline (read this first).** Every substantive claim is tagged:

- **[ZED]** — a statement by Zed Industries in a primary source (their blog, product docs, roadmap, or a direct quote from their CEO in an interview).
- **[3P]** — journalist / third-party interpretation, or a community claim. Not authoritative.
- **[INFERENCE]** — my own reasoning, explicitly not stated by anyone.

Where a fact could not be confirmed from a primary source, it is listed in **§8 Unconfirmed** rather than asserted. Where Zed contradicts or narrows its own marketing, that is called out.

---

## 0. Source inventory (with dates)

| # | Source | Type | Date |
|---|--------|------|------|
| S1 | [Sequoia Backs Zed's Vision for Collaborative Coding](https://zed.dev/blog/sequoia-backs-zed) — first public DeltaDB description ("Introducing DeltaDB: Operation-Level Version Control") | **ZED** | 2025-08-20 |
| S2 | [Zed is 1.0](https://zed.dev/blog/zed-1-0) — "character-level granularity" claim | **ZED** | 2026-04-29 |
| S3 | [Software Is Made Between Commits](https://zed.dev/blog/introducing-deltadb) — the DeltaDB announcement | **ZED** | 2026-06-11 |
| S4 | [DeltaDB — Early Access](https://zed.dev/deltadb) — four capability bullets | **ZED** | live (waitlist opened ~2026-06-11) |
| S5 | [Introducing Delta](https://zed.dev/blog/introducing-delta) — first client app, Git interop | **ZED** | 2026-08-12 |
| S6 | [delta.dev](https://delta.dev) — product site | **ZED** | live |
| S7 | [Delta Docs — Getting Started](https://delta.dev/docs/getting-started) | **ZED** | live |
| S8 | [Delta Docs — Core Concepts](https://delta.dev/docs/concepts/core-concepts) | **ZED** | live |
| S9 | [Delta Docs — Delta & Git](https://delta.dev/docs/concepts/delta-and-git) | **ZED** | live |
| S10 | [Delta Docs — Delta Worktrees & Machines](https://delta.dev/docs/concepts/worktrees-and-machines) | **ZED** | live |
| S11 | [Delta Docs — Reviewing & Syncing Changes](https://delta.dev/docs/agents/review-and-sync) | **ZED** | live |
| S12 | [Delta Docs — Comments](https://delta.dev/docs/agents/comments) | **ZED** | live |
| S13 | [Delta Docs — Threads](https://delta.dev/docs/agents/threads) | **ZED** | live |
| S14 | [Delta Docs — Collaborate in a Thread](https://delta.dev/docs/collaboration/collaborate-thread) | **ZED** | live |
| S15 | [Delta Docs — Delta on the Web](https://delta.dev/docs/collaboration/delta-on-the-web) | **ZED** | live |
| S16 | [Delta Docs — Data Storage & Deletion](https://delta.dev/docs/privacy-and-security/data-storage) | **ZED** | live |
| S17 | [Delta Docs — Release notes](https://delta.dev/docs/whats-in-the-latest) (through 0.11.0) | **ZED** | 2026-09-11 |
| S18 | [Delta Roadmap](https://delta.dev/roadmap) | **ZED** | live |
| S19 | [Zed Roadmap](https://zed.dev/roadmap) | **ZED** | live |
| S20 | [How CRDTs make multiplayer text editing part of Zed's DNA](https://zed.dev/blog/crdts) | **ZED** | 2022-12-01 |
| S21 | [Hanselminutes #1049 — The space between the Commits with Nathan Sobo](https://hanselminutes.com/1049/the-space-between-the-commits-with-zed-and-deltadbs-nathan-sobo) | **ZED** (interview) | 2026-06-18 |
| S22 | [GitHub Discussion #55316 — "DeltaDB is already DONE…"](https://github.com/zed-industries/zed/discussions/55316) | **3P** (community, Show and tell) | undated in fetched view |
| S23 | [TechTimes — Zed Opens DeltaDB Waitlist](https://www.techtimes.com/articles/318322/20260613/zed-opens-deltadb-waitlist-crdt-version-control-records-every-edit-not-just-commits.htm) | **3P** | 2026-06-13 |
| S24 | [GIGAZINE — Zed has announced 'DeltaDB'](https://gigazine.net/gsc_news/en/20260612-zed-deltadb) | **3P** | 2026-06-12 |
| S25 | [BlockBeats — Operation Log instead of Commit](https://en.theblockbeats.news/flash/350936) | **3P** | ~2026-06-11/12 |
| S26 | [AlphaSignal — Zed's DeltaDB Rebuilds Version Control Around AI Agent Conversations](https://alphasignal.ai/news/zed-s-deltadb-rebuilds-version-control-around-ai-agent-conversations) | **3P** | 2026-06-11 |
| S27 | [RuntimeWire — Nathan Sobo's Zed takes aim at pull requests with DeltaDB](https://runtimewire.com/article/zed-deltadb-version-control-agent-conversations) | **3P** | 2026-06-11 |
| S28 | [Nitin Kumar Singh — Zed and DeltaDB: Version Control Between Commits](https://nitinksingh.com/updates/zed-and-deltadb-version-control-between-commits/) | **3P** | 2026-07-08, upd. 2026-08-21 |
| S29 | [技术栈 — 软件诞生于 Commit 之间：聊聊 Zed 的 DeltaDB](https://jishuzhan.net/article/2096521091758608386) | **3P** | 2026-09-06 |
| S30 | [Sesame Disk — What Is Zed DeltaDB and Its Key Features](https://sesamedisk.com/what-is-zed-deltadb-features) | **3P** | 2026-08-06 |
| S31 | [DEV/elsolitario — Zed lanza DeltaDB](https://dev.to/lu1tr0n/zed-lanza-deltadb-control-de-versiones-que-graba-cada-cambio-entre-commits-1ja1) | **3P** | 2026-08-06 |
| S32 | [HN #48492533](https://news.ycombinator.com/item?id=48492533) — announcement thread, 225 comments | **3P** (community) | 2026-06-11 |
| S33 | [HN #49187256](https://news.ycombinator.com/item?id=49187256) — follow-up thread | **3P** (community) | 2026-08-05 |
| S34 | [Gus Mueller — DeltaDB From Zed](https://shapeof.com/archives/2025/8/deltadb_from_zed.html) | **3P** | 2025-08-20 |

---

## 1. The core abstraction: what is a "delta"?

### [ZED] Primary statements

The Delta documentation defines it plainly:

> "A delta is a recorded change to a thread or Delta worktree. It can represent a file edit, a change to the file tree, a message, a comment, or another update to shared state. Each delta records who made the change and where it belongs in the thread's history. This allows DeltaDB to combine changes from different participants and keep every copy synchronized." — [S9](https://delta.dev/docs/concepts/delta-and-git)

> "Unlike a git commit, a delta is created continuously as work happens. You do not need to stage or commit it." — [S9](https://delta.dev/docs/concepts/delta-and-git)

So a delta is **not** only a code edit. It is the unit of *any* shared-state change in a thread: file edits, file-tree changes, messages, comments, and "another update to shared state." Code changes and conversation are the *same kind of thing* in the data model. That is the central design move.

The announcement describes the identity property:

> "DeltaDB breaks your work into a stream of fine-grained *deltas*. Where Git captures a snapshot at each commit, DeltaDB captures every operation in between and gives each one a stable identity. Because every delta can be addressed on its own, you can point to the code at any moment in its evolution, even as it keeps changing." — [S3](https://zed.dev/blog/introducing-deltadb)

> "DeltaDB captures every operation in between commits and gives each one a stable identity, so you can point to the code at any moment in its evolution." — [S4](https://zed.dev/deltadb)

> "DeltaDB records each edit, from each person and the agent, in the order it is made." — [S9](https://delta.dev/docs/concepts/delta-and-git)

Two properties are therefore claimed by Zed directly:

1. **Stable identity** — each delta has an identity that does not change.
2. **Individually addressable** — a delta can be referenced on its own, which is what makes "point to the code at any moment" possible.

**Granularity.** Zed says "every operation in between" ([S3](https://zed.dev/blog/introducing-deltadb)) and, earlier, "tracks every change with **character-level granularity**" ([S2](https://zed.dev/blog/zed-1-0), 2026-04-29). The Sequoia post calls it "fine-grained change tracking … **character-level permalinks** that survive any code transformation" ([S1](https://zed.dev/blog/sequoia-backs-zed)).

Note the tension: `character-level granularity` is a claim about the *substrate* inherited from Zed's text CRDTs ([S20](https://zed.dev/blog/crdts) describes Zed's editor buffers as CRDTs whose insertions get `(replica_id, sequence_number)` ids and whose anchors are `(insertion id, offset)` pairs). The DeltaDB docs, however, never say "a delta is a keystroke." A delta is defined as a recorded *change to shared state*, and the docs describe messages, comments and file-tree changes as deltas too. **The precise mapping between "delta" and "character insertion" is not published.** See §8.

### [3P] Third-party readings

- GIGAZINE: "A key feature of DeltaDB is that it continuously records small changes that occur during development as 'deltas.' … each operation is assigned a stable identifier." — [S24](https://gigazine.net/gsc_news/en/20260612-zed-deltadb)
- BlockBeats: "DeltaDB records every fine-grained edit operation (Delta) and assigns a unique identifier." — [S25](https://en.theblockbeats.news/flash/350936)
- The `#55316` discussion quotes (in Russian and English) a Habr article about the Zed 1.0 release: *"DeltaDB — operation-based version control system built on CRDT. Tracks edits with single-character granularity…"* — [S22](https://github.com/zed-industries/zed/discussions/55316). The same post **disputes** character granularity as the right unit: "character-level granularity is the wrong unit for code. A single keystroke isn't a meaningful change — it produces noisy history, weak conflict resolution, and semantically broken intermediate states." This is a community author's opinion, not Zed's.
- The Chinese technical write-up frames the whole system as an **event-sourced database**: "DeltaDB 本质上是一个编程行为的事件源数据库 (event-sourced database). … Git 的 DAG 里每个节点是一个 commit 快照，DeltaDB 的 DAG 里每个节点是一个具体发生的事件——一次编辑、一条对话消息、一次 prompt." — [S29](https://jishuzhan.net/article/2096521091758608386). It attributes this framing to the Hanselminutes interview [S21](https://hanselminutes.com/1049/the-space-between-the-commits-with-zed-and-deltadbs-nathan-sobo). I could not retrieve the episode transcript, so treat "event-sourced database" as **attributed paraphrase of a primary interview, not a transcript-verified quote**.

### [INFERENCE]

If deltas are events with stable IDs, the only workable ID scheme is one that can be generated **without coordination** at each replica (otherwise offline/concurrent edits stall), i.e. something like `(replica/participant id, monotonic counter)`, optionally combined with a hash of content. Zed's editor CRDT uses exactly that shape ([S20](https://zed.dev/blog/crdts)). But **DeltaDB docs never publish its delta ID format**, and there is no evidence that deltas are content-addressed (see §3 and §8).

---

## 2. References / anchors: what "anchored to a delta instead of a line number" means

### [ZED] Primary statements

The core claim, verbatim:

> "Because every reference is anchored to a delta instead of a line number, it survives as the code moves underneath it. From any line in a past conversation, you can jump to that code as it stands now or as it stood the moment the agent wrote it. From any line of code, you can find the conversation that produced it and every conversation that has touched it since." — [S3](https://zed.dev/blog/introducing-deltadb)

> "From any line of code, find the conversation. From any message, jump to the code it touched." — [S4](https://zed.dev/deltadb)

> "Fine-grained change tracking also enables character-level permalinks that survive any code transformation, so we can anchor our interactions to arbitrary locations in the codebase, not just to snapshots of recently-changed code." — [S1](https://zed.dev/blog/sequoia-backs-zed)

Agents are also consumers:

> "Agents can draw on it too. They pick up the context behind the code they're touching or convene the prior agents that worked on it and ask why it's written the way it is." — [S3](https://zed.dev/blog/introducing-deltadb)

**What this buys, in Zed's own framing**, is bidirectional, drift-resistant traceability: *code → conversation* and *conversation → code*, at two time coordinates ("as it stands now" and "as it stood the moment the agent wrote it").

### Important qualifier: this capability is on the roadmap as *In Progress*

The Delta roadmap lists, under Public Beta:

> "**Navigate from code to conversation** — *In Progress* — Give humans and agents access to the conversations that shaped the code, with links that stay anchored as the code evolves." — [S18](https://delta.dev/roadmap)

So as of the fetched roadmap, part of the anchor/trace capability is *still being built*, not shipped. The announcement language describes the intended end state.

### Observable anchor behavior in current docs (primary)

The docs do not describe the anchor data structure, but they reveal behavior under change:

- Comments attach to selected spans of transcript text or to selected code in file/diff views; once delivered they "collapse into indicators in the gutter." — [S12](https://delta.dev/docs/agents/comments)
- Release notes 0.11.0: *"Comments on deleted text no longer appear attached to replacement text in file views, while remaining available in the thread transcript."* — [S17](https://delta.dev/docs/whats-in-the-latest). This is direct evidence that anchors into **deleted** text are handled specially: the reference is preserved in the log, but it is not re-pointed at whatever text replaced it.
- Revert: "Place the cursor at the point you want to return to… It restores attached Delta worktrees that still match that point; worktrees that now use a different checkout are left unchanged." — [S13](https://delta.dev/docs/agents/threads). So a "point in a conversation" is also a point in worktree history, and reverting is conditional on the worktree still matching.

### [3P] "Portals"

Two secondary write-ups say Nathan Sobo described a feature called **portals** in the Hanselminutes interview: clicking a code reference inside an agent transcript takes you to the code *as the agent saw it at the moment of the reference*, not the current code, and your cursor stays attached to that logical location as you scrub time forward even after the code has moved to another file.

- [S28](https://nitinksingh.com/updates/zed-and-deltadb-version-control-between-commits/): "Nathan describes portals: when an agent references code in a conversation, you step into the exact code at the exact moment the reference was made, and your cursor stays attached to that logical location as you scrub time forward, even after the code moves to another file, because DeltaDB knows the operations that moved it. That is closer to a causal index over the session than to a better `git blame`."
- [S29](https://jishuzhan.net/article/2096521091758608386): same claim, in Chinese, citing the same episode [S21](https://hanselminutes.com/1049/the-space-between-the-commits-with-zed-and-deltadbs-nathan-sobo).

I could not verify the transcript myself (see §8). Treat "portals" as **reported, not primary-verified**. Note that no `delta.dev` doc I found uses the word "portal."

### [INFERENCE]

"Anchored to a delta instead of a line number" only works if three things are true, none of which Zed has published:

1. **Tombstones / immutable insert history.** To resolve an anchor whose text was deleted, you must retain the deleted text or a mapping through it. Zed's editor CRDT does exactly this (deletions are tombstones; anchors are `(insertion id, offset)`) — [S20](https://zed.dev/blog/crdts). That is a strong hint about the substrate, not a DeltaDB confirmation.
2. **File-tree identity across renames/moves.** "Survives as the code moves underneath it" implies file moves are tracked as deltas too — consistent with the docs calling "a change to the file tree" a delta ([S9](https://delta.dev/docs/concepts/delta-and-git)), but the resolution algorithm is unpublished.
3. **Re-resolution cost control.** Mapping N anchors onto the current tree on every read is potentially expensive; the release notes' language about "checkpoints" and avoiding "increasingly expensive replay" (see §3) suggests a checkpoint+index design. This is inference from release-note wording.

---

## 3. Storage / versioning model

### [ZED] What is stored

DeltaDB is described as the shared database behind a thread:

> "DeltaDB is the shared database behind a Delta thread. It records thread messages, comments, Delta worktree changes, and file edits as deltas. Agents can read and edit a Delta worktree's shared file representation in DeltaDB. Delta also materializes those files in normal folders called checkouts… DeltaDB records the edits and synchronizes them between checkouts." — [S9](https://delta.dev/docs/concepts/delta-and-git)

Four categories of server-side data, with concrete infrastructure:

> - **Repository contents and history.** The Git objects for repositories attached to your threads, including commits and file contents.
> - **The thread's deltas.** … Delta stores these changes **in sequence** so it can reconstruct each thread and keep copies synchronized across machines.
> - **Metadata.** … shareable thread record (key to join, display name), thread catalog, Git remote URL, collaborator GitHub logins, commit author/committer identities and messages.
> - **Subscription credentials.** — [S16](https://delta.dev/docs/privacy-and-security/data-storage)

| Data | Storage |
|---|---|
| File contents, Git commits, **history packs, and checkpoints** | Cloudflare R2 |
| Thread and Delta worktree **deltas used for active synchronization** | Cloudflare Durable Objects backed by SQLite |
| Thread, repository, collaborator, account metadata | Cloudflare KV and D1 |

— [S16](https://delta.dev/docs/privacy-and-security/data-storage)

"Encrypted at rest." Backend "runs entirely on Cloudflare." — [S16](https://delta.dev/docs/privacy-and-security/data-storage)

### [ZED] Snapshot vs. log

Zed is explicit that the model is **log-first, commit-free**:

> "Git records a snapshot of the repository each time someone commits. DeltaDB records each edit, from each person and the agent, in the order it is made… None of it involves a commit." — [S9](https://delta.dev/docs/concepts/delta-and-git)

But the infrastructure list above names **checkpoints** and **history packs** alongside deltas. The release notes corroborate a checkpoint/replay hybrid:

> "Large collaborative threads now continue publishing **checkpoints** after **timeline fork metadata** changes, avoiding increasingly expensive **replay** when opening them." — 0.6.0 release notes, [S17](https://delta.dev/docs/whats-in-the-latest)

> "New thread histories now write far less to the local database as they are edited, reducing disk activity in heavily edited threads." — [S17](https://delta.dev/docs/whats-in-the-latest)

### [INFERENCE]

The architecture is almost certainly a **operation log + periodic materialized checkpoints**, not a pure log and not a snapshot store:

- "Deltas … stored in sequence so it can reconstruct each thread" = replayable log.
- "Checkpoints" stored in R2 as durable blobs = materialized state at a prefix of the log.
- "Avoiding increasingly expensive replay when opening" = opening a thread = load latest checkpoint + replay the tail.
- "Timeline fork metadata" = branching/forking at a log position, consistent with "any point in history is a valid branch point" ([S4](https://zed.dev/deltadb)).
- The docs' two-representation model (a "shared file representation" inside DeltaDB plus on-disk checkouts) is a *third* materialization axis on top of log/checkpoint.

This is a standard event-sourcing design. It is not stated as such by Zed.

### Materializing a worktree version

Primary statements:

- "A Delta worktree is a project's files and recorded history in DeltaDB. … A **checkout** is the folder on one machine where that Delta worktree's files exist as real files on disk. Your checkout is not the only copy: every participant has their own, synced through DeltaDB." — [S10](https://delta.dev/docs/concepts/worktrees-and-machines)
- "The shared representation and its checkouts are two ways to work with the same Delta worktree. The agent's direct file operations can use the representation in DeltaDB; terminal commands and other filesystem-based tools run in a checkout. Delta keeps both synchronized." — [S9](https://delta.dev/docs/concepts/delta-and-git)
- "DeltaDB virtualizes the worktree, so spinning up a new agent branch is effectively free. Any point in history is a valid branch point, including mid-run." — [S4](https://zed.dev/deltadb)
- "The files are real: agents work in them through a terminal, and you can mount the whole worktree to disk whenever you want your own tools on it." — [S3](https://zed.dev/blog/introducing-deltadb)
- Fidelity detail: "Delta preserves each file's line endings when it writes a checkout and honors Git's `core.autocrlf`, `core.eol`, and `.gitattributes` conversion rules." — [S9](https://delta.dev/docs/concepts/delta-and-git)
- Checkout bookkeeping lives in a `.delta` folder inside the repository, "whose machine-local contents are hidden from git automatically"; managed checkouts live under `.delta/worktrees/` unless the project was joined without a local clone. — [S10](https://delta.dev/docs/concepts/worktrees-and-machines)
- Checkouts "share object storage to save disk space, but edits and commits in one do not update another's same-named branch." — [S10](https://delta.dev/docs/concepts/worktrees-and-machines)

### Are deltas content-addressed?

**Not stated anywhere in the primary sources.** What *is* stated is that **Git objects** ("commits and file contents") are stored server-side ([S16](https://delta.dev/docs/privacy-and-security/data-storage)) — so content-addressing exists in the Git layer that DeltaDB carries, but the docs never say deltas are keyed by content hash. See §8.

### Retention / pruning / garbage collection

Primary facts that exist:

- **Local history eviction:** "Archived threads that are synced to our servers drop their local history after three days to free disk space; opening one downloads its history again. Threads that were never synced keep their local history until you delete them." — [S16](https://delta.dev/docs/privacy-and-security/data-storage)
- **Thread deletion, not delta GC:** `delta cli thread delete` and `delta cli thread purge` exist; "`purge` only prints what would be lost until you pass `--force`; both require Delta to be closed." Deleting an uploaded/joined thread removes the local copy on every signed-in device but **"does not yet remove the thread's history from Delta's servers."** — [S16](https://delta.dev/docs/privacy-and-security/data-storage)
- **Subagent checkout cleanup:** "Finished subagents now use less disk space by automatically cleaning up idle checkouts and restoring them when needed for follow-up work." — 0.8.0 release notes, [S17](https://delta.dev/docs/whats-in-the-latest). This is *checkout* GC, not delta GC.
- **What deletion cannot reach:** upstream repos, other people's clones, already-shared content; "We do not rewrite thread or git history." — [S16](https://delta.dev/docs/privacy-and-security/data-storage)

**Delta-level compaction, tombstone collection, or log truncation is never described.** This is the single most-flagged gap by third parties:

> "Zed has not published how it plans to handle that history's growth or whether there will be any pruning, summarization, or garbage collection mechanism." — [S30](https://sesamedisk.com/what-is-zed-deltadb-features)

> "El riesgo… es el volumen de datos… Zed no publicó todavía cómo planea manejar el crecimiento de ese historial ni si habrá algún mecanismo de poda o resumen." — [S31](https://dev.to/lu1tr0n/zed-lanza-deltadb-control-de-versiones-que-graba-cada-cambio-entre-commits-1ja1)

### [INFERENCE]

A log-first design typically *cannot* discard deltas without either (a) rewriting anchors that point into them, or (b) keeping a durable ID→current-location index and expiring content. Neither policy is documented. For any portable adoption of these ideas, the retention policy is the part you must design yourself; DeltaDB does not hand you one.

---

## 4. Concurrency: what the CRDT layer provides, and what it does not

### [ZED] Primary claims

> "Because DeltaDB embeds **conflict-free replicated worktrees**, many people and agents can edit the same files at once across different machines." — [S3](https://zed.dev/blog/introducing-deltadb)

> "DeltaDB uses CRDTs to incrementally record and synchronize changes as they happen. It's designed to interoperate with Git, but its operation-based design supports real-time interactions that aren't supported by Git's snapshots." — [S1](https://zed.dev/blog/sequoia-backs-zed)

> "We're actively developing DeltaDB, a synchronization engine built on CRDTs that tracks every change with character-level granularity. DeltaDB lets multiple humans and agents share a single, consistent view of the codebase as it evolves." — [S2](https://zed.dev/blog/zed-1-0)

> "DeltaDB makes the worktree itself collaborative. Every participant gets their own copy of the code on their local machine, kept in sync in real time as the work happens." — [S5](https://zed.dev/blog/introducing-delta)

> "Delta gives every participant a full copy of the thread: the conversation, the file history, and a checkout of each Delta worktree. Like a Google Doc, the copies stay in sync." — [S10](https://delta.dev/docs/concepts/worktrees-and-machines)

So the CRDT layer is claimed to provide: **replicated worktrees**, per-participant full copies, **real-time** bidirectional sync, and a "single, consistent view." The word "conflict-free" is Zed's.

### [ZED] Explicit guarantees *not* offered (these are the important ones)

Zed's docs are unusually candid about boundaries, which is more useful than the marketing:

| DeltaDB synchronizes | Stays on the execution machine |
|---|---|
| Conversation, comments, and agent output | Running processes and application state |
| Delta worktree files and recorded history | Development servers, local databases, local network services |
| Recorded file changes | Environment variables, credentials, machine-local MCP servers, installed tools |
| | Git-ignored files, including local `.env` files |

— [S10](https://delta.dev/docs/concepts/worktrees-and-machines)

> "Delta syncs the conversation and recorded file changes, but it does not sync machine-local runtime state." — [S14](https://delta.dev/docs/collaboration/collaborate-thread)

Other explicit limits:

- **Git-level conflicts remain between separate threads:** "To combine independent work, bring the commits into the same repository before merging or rebasing. **Conflicts can still occur.**" — [S10](https://delta.dev/docs/concepts/worktrees-and-machines)
- **Shared checkouts are not isolated:** "Threads that use the same existing local checkout are not isolated. Edits made in either thread appear in the shared Delta worktree, and agents running at the same time can modify the same files." — [S10](https://delta.dev/docs/concepts/worktrees-and-machines)
- **`.gitignore` is load-bearing:** ignored files are "not recorded or synced, so build artifacts and secrets such as `.env` files stay on the machine they are on." — [S9](https://delta.dev/docs/concepts/delta-and-git)
- **Execution locality:** "A local turn cannot run on another participant's computer." Cloud turns run on a separate machine per participant, with that participant's credentials. — [S10](https://delta.dev/docs/concepts/worktrees-and-machines), [S14](https://delta.dev/docs/collaboration/collaborate-thread)
- **Write-back honesty:** "Delta preserves concurrent file edits when writing to a local checkout, and reports an error rather than claiming success if the checkout keeps changing." — 0.9.0 release notes, [S17](https://delta.dev/docs/whats-in-the-latest)

### [3P] What third parties argue the CRDT layer does *not* solve

The strongest technical critique is about **semantic** (not data) convergence. Quoted in [S29](https://jishuzhan.net/article/2096521091758601386) from a LinkedIn post by Mike Freedman: at the data layer, CRDT merge is consistent; at the code layer, the merged result is often code that does not compile. A "conflict-free" state that fails to compile is *more* dangerous than a visible conflict, because nobody is alerted. [S29](https://jishuzhan.net/article/2096521091758601386) also notes Freedman's counterpoint: when the merger is an AI agent that can compile, test, and repair, the calculus can change.

Other third-party caveats worth recording (all [3P]):

- Real-time editing of the *same* file by humans and agents is described as an eventual-consistency property, and independent verification is absent: "There are no benchmarks, no third-party testing, no real-world deployment reports." — [S30](https://sesamedisk.com/what-is-zed-deltadb-features)
- HN commenters on the announcement argued Git can already do fine-grained history via frequent auto-commits, `--first-parent`, and `merge --no-ff`, and that "the mess in between working software is not something I care to have version controlled" — [S32](https://news.ycombinator.com/item?id=48492533). (This is opinion, not fact.)
- Several commenters raised the storage/bandwidth cost of shipping every operation to every replica — [S32](https://news.ycombinator.com/item?id=48492533), [S30](https://sesamedisk.com/what-is-zed-deltadb-features).
- Prior-art comparisons offered by commenters: Google's Piper/citc (decades of "Ctrl-S granularity" history), JetBrains Local History, Jujutsu (`jj`), Dolt, Fossil, Beads+Dolt — [S32](https://news.ycombinator.com/item?id=48492533). These are useful for a "has this been done" prior-art check but carry no authority about DeltaDB.

### [INFERENCE]

"Conflict-free replicated worktree" is a **claim about data convergence, not about code correctness**. A tool that plans to adopt this must decide separately what it does when converged state is semantically broken. DeltaDB's own answer appears to be: route it through Git and let Git produce a conflict ([S10](https://delta.dev/docs/concepts/worktrees-and-machines)) — i.e. CRDT for the *in-between* state, Git's conflict machinery for the *publication* state.

---

## 5. Relationship to Git

DeltaDB is explicitly positioned as **complementary to Git**, and the docs go much further than the blog in specifying the boundary.

### [ZED] What stays in Git

> "Git and CI stay for what they're good at: running checks and connecting you to the rest of the world, rather than being the place collaboration is forced to happen." — [S3](https://zed.dev/blog/introducing-deltadb)

> "DeltaDB works with the git repository you already have. Every edit and conversation is captured *between* your commits. **You can commit and push like you always did, and teammates who never open Delta see a normal git repo.**" — [S5](https://zed.dev/blog/introducing-delta)

> "Your repository stays a normal git repository: commits, branches, and remotes work as before. … Commits stay in git." — [S8](https://delta.dev/docs/concepts/core-concepts), [S9](https://delta.dev/docs/concepts/delta-and-git)

### [ZED] What moves to DeltaDB

- File edits as they happen (no staging/commit): [S9](https://delta.dev/docs/concepts/delta-and-git)
- File-tree changes: [S9](https://delta.dev/docs/concepts/delta-and-git)
- Thread messages and agent output: [S9](https://delta.dev/docs/concepts/delta-and-git), [S16](https://delta.dev/docs/privacy-and-security/data-storage)
- Comments and review verdicts: [S11](https://delta.dev/docs/agents/review-and-sync), [S12](https://delta.dev/docs/agents/comments)
- Conversation revert (which also restores attached worktrees): [S13](https://delta.dev/docs/agents/threads)
- The conversation↔code index itself: [S3](https://zed.dev/blog/introducing-deltadb)

### [ZED] Concrete interop mechanics

- **Project requirement:** "The folder must be a git repository, or a folder inside one." Delta imports files Git does not ignore; "skipping folders whose contents are entirely untracked, such as build output; `git add` brings a skipped folder in." — [S9](https://delta.dev/docs/concepts/delta-and-git)
- **Two remotes** on a managed checkout:
  - `origin` — "the shared upstream your repository already points at, such as GitHub."
  - `local` — "your own repository on your machine. The agent can push a branch to local to hand you work without a round trip through GitHub, including the branch you have checked out: a clean checkout updates in place, and git rejects a push that would overwrite local edits."
  — [S9](https://delta.dev/docs/concepts/delta-and-git)
- **Four ways work reaches you:** adopt your folder in place; `git push local <branch>`; push to `origin` / open a PR; or open the managed checkout folder directly. — [S9](https://delta.dev/docs/concepts/delta-and-git), [S11](https://delta.dev/docs/agents/review-and-sync)
- **Sharing requires a remote:** "Sharing requires the project's repository to have a Git remote. If it has none, add a remote and push it, then share again." — [S14](https://delta.dev/docs/collaboration/collaborate-thread)
- **Git objects are replicated by DeltaDB:** "Delta moves them between machines as needed, so a collaborator's machine can get your current commit without a round trip through origin." — [S9](https://delta.dev/docs/concepts/delta-and-git)
- **Commit diffs backlink to the thread:** "Commit diffs now link to the shared Delta thread that produced the commit when one is available." — 0.7.0 release notes, [S17](https://delta.dev/docs/whats-in-the-latest)
- **Agent harnesses other than Delta:** "Delta also connects to third-party agent harnesses, starting with Claude Code. Keep working in the terminal you already use, and your session syncs live into a Delta thread." — [S5](https://zed.dev/blog/introducing-delta); roadmap item "Claude Code plugin — Track Claude Code conversations and code changes in DeltaDB, then review them on delta.dev" (In Progress) — [S18](https://delta.dev/roadmap)
- **Jujutsu:** early support for colocated `jj` repos; Delta runs `jj git init --colocate` in managed checkouts and preserves/restores the Jujutsu op log across archive/mount. "Jujutsu support remains incomplete." — [S9](https://delta.dev/docs/concepts/delta-and-git)
- **Planned:** "Expanded Git workflows — Review, stage, and commit changes across repositories, then open a pull request from Delta" (GA, Up Next) — [S18](https://delta.dev/roadmap)

### [3P] Third-party read on the Git relationship

RuntimeWire framed it accurately: "Zed is not saying Git disappears… Zed's critique is that the highest-value discussion often happened earlier, while the worktree was changing… **Capturing the work before Git sees it is the near-term wedge.**" — [S27](https://runtimewire.com/article/zed-deltadb-version-control-agent-conversations)

### [INFERENCE]

The division of labor is:
- **Git = publication + ecosystem boundary** (commit, branch, remote, CI, PR, code host).
- **DeltaDB = the private, high-resolution, shared *pre-publication* layer** (every edit, every message, per-participant replicated worktrees).

The important consequence for portability: **DeltaDB is not a VCS in the "replace Git" sense.** It does not define a wire format for history, it is not itself pushable, and Git is what makes the content addressable and publishable. There is **no public DeltaDB protocol or read API** documented; the only client is Delta, plus the in-progress Claude Code bridge.

---

## 6. Primary vs. third-party vs. inference — consolidated

**Statements from Zed (primary), safe to rely on:**

- A delta is any recorded change to a thread or Delta worktree: file edit, file-tree change, message, comment, or other shared-state update; each delta records *who* and *where in thread history*. ([S9](https://delta.dev/docs/concepts/delta-and-git))
- Deltas are created continuously; no stage/commit. ([S9](https://delta.dev/docs/concepts/delta-and-git))
- Each delta has a stable identity and is individually addressable. ([S3](https://zed.dev/blog/introducing-deltadb), [S4](https://zed.dev/deltadb))
- References are anchored to deltas, not line numbers, and survive code motion; conversation→code and code→conversation both work, at "then" and "now" coordinates. ([S3](https://zed.dev/blog/introducing-deltadb))
- "Character-level" granularity and "character-level permalinks that survive any code transformation." ([S1](https://zed.dev/blog/sequoia-backs-zed), [S2](https://zed.dev/blog/zed-1-0))
- DeltaDB embeds conflict-free replicated worktrees; per-participant copies kept in sync in real time; "like a Google Doc." ([S3](https://zed.dev/blog/introducing-deltadb), [S5](https://zed.dev/blog/introducing-delta), [S10](https://delta.dev/docs/concepts/worktrees-and-machines))
- The worktree is virtualized; any point in history, including mid-run, is a valid branch point; branches are "effectively free." ([S4](https://zed.dev/deltadb))
- Files are real on disk in checkouts, mountable, and usable by terminals/other tools. ([S3](https://zed.dev/blog/introducing-deltadb), [S9](https://delta.dev/docs/concepts/delta-and-git))
- Git and CI stay; commits stay in Git; a teammate without Delta sees a normal Git repo; `local`/`origin` remote split. ([S3](https://zed.dev/blog/introducing-deltadb), [S5](https://zed.dev/blog/introducing-delta), [S9](https://delta.dev/docs/concepts/delta-and-git))
- `.gitignore` governs what is recorded/synced. ([S9](https://delta.dev/docs/concepts/delta-and-git))
- Server stack: Cloudflare R2 (contents, Git objects, history packs, checkpoints), Durable Objects+SQLite (active-sync deltas), KV+D1 (metadata); encrypted at rest. ([S16](https://delta.dev/docs/privacy-and-security/data-storage))
- Retention: 3-day local eviction of archived+synced threads; server copy remains. ([S16](https://delta.dev/docs/privacy-and-security/data-storage))
- No sync of machine-local runtime state, credentials, ignored files, running processes. ([S10](https://delta.dev/docs/concepts/worktrees-and-machines))
- Cross-thread combination still uses Git merge/rebase and "conflicts can still occur." ([S10](https://delta.dev/docs/concepts/worktrees-and-machines))
- Zed plans to open-source DeltaDB with an optional paid service. ([S1](https://zed.dev/blog/sequoia-backs-zed))

**Third-party interpretation (corroborating, but not authority):**

- "Operation log instead of commit"; "assigns a unique identifier"; anchors on deltas not line numbers — BlockBeats ([S25](https://en.theblockbeats.news/flash/350936)).
- CRDT = eventual consistency without a coordinator; storage/complexity trade-off — [S30](https://sesamedisk.com/what-is-zed-deltadb-features), [S26](https://alphasignal.ai/news/zed-s-deltadb-rebuilds-version-control-around-ai-agent-conversations).
- "Event-sourced database" framing and "portals" — attributed to the Hanselminutes interview via [S29](https://jishuzhan.net/article/2096521091758608386) and [S28](https://nitinksingh.com/updates/zed-and-deltadb-version-control-between-commits/); **transcript not verified by me**.
- "Character-level granularity is the wrong unit" critique — community author on GitHub ([S22](https://github.com/zed-industries/zed/discussions/55316)).
- Code-level CRDT merges can fail to compile — Mike Freedman via [S29](https://jishuzhan.net/article/2096521091758601386).
- Data growth/GC, benchmarks, editor lock-in are unresolved — [S30](https://sesamedisk.com/what-is-zed-deltadb-features), [S26](https://alphasignal.ai/news/zed-s-deltadb-rebuilds-version-control-around-ai-agent-conversations), [S31](https://dev.to/lu1tr0n/zed-lanza-deltadb-control-de-versiones-que-graba-cada-cambio-entre-commits-1ja1).
- HN skepticism/prior art (Piper, jj, Dolt, JetBrains Local History, auto-commit+`--first-parent`) — [S32](https://news.ycombinator.com/item?id=48492533), [S33](https://news.ycombinator.com/item?id=49187256).

**My inference (not stated by anyone):**

- Log + periodic checkpoints + replay-on-open, with fork metadata for branching (§3).
- Delta IDs must be coordination-free (replica-id + counter), but the format is unpublished (§1).
- Tombstones/immutable insertion history are required for delta-anchors, and Zed's editor CRDT already has that shape (§2).
- CRDT gives data convergence only; semantic correctness is out of scope of the CRDT claim (§4).
- DeltaDB's real role is the pre-publication layer under Git (§5).

---

## 7. Portable primitives

The goal here: ideas that are **architecture-independent** and usable by a tool that is *not* an editor, *not* a VCS, and *not* multi-machine — for example a CLI that verifies whether recorded evidence (file paths, line ranges, content hashes) is still true in a working tree.

For each primitive: what it is, what it buys, what it costs, and whether DeltaDB actually demonstrates it or merely claims it.

---

### P1. Addressable events with coordination-free IDs

**Idea.** Make every recorded change a first-class object with a stable ID generated locally (e.g. `(producer_id, counter)`), never a position in a file or a queue index. All later references point at the ID, not at a location.

- **Buys:** references that survive reordering, rewriting, and replay; you can build indexes over history without a central allocator; offline/parallel producers work.
- **Costs:** you now need a mapping from ID → current location (an index you must maintain); IDs are opaque, so debugging requires tooling; you must decide a scope for uniqueness (per repo? per thread? global?).
- **DeltaDB status:** explicitly claimed ("stable identity", "addressed on its own", [S3](https://zed.dev/blog/introducing-deltadb), [S4](https://zed.dev/deltadb)). ID *format* unpublished.
- **Portability:** high. A verification CLI can mint `evidence-id`s the same way and store them in the record.

### P2. Anchors as `(origin-id, offset)` instead of `(path, line-range)`

**Idea.** A reference into text is stored as *a pointer into an immutable insertion, plus an offset*, not as a line number. Line numbers are resolved at read time.

- **Buys:** a reference does not break when lines are inserted above it; you can render both "where it is now" and "where it was then"; you can support "find all records touching this line" by inverting the index.
- **Costs:** you must retain deleted/tombstoned text (or an equivalent mapping) to resolve anchors that point into it; you need a resolver and an index to avoid O(history) scans; you inherit the problem of **file identity across renames** (an anchor into a moved file needs a stable file/tree identity too).
- **DeltaDB status:** claimed at product level ([S3](https://zed.dev/blog/introducing-deltadb), [S1](https://zed.dev/blog/sequoia-backs-zed)); the underlying `(insertion id, offset)` anchor mechanism is documented only for **Zed's editor CRDT** ([S20](https://zed.dev/blog/crdts)), not confirmed for DeltaDB. Observable behavior under deletion is documented ([S17](https://delta.dev/docs/whats-in-the-latest)).
- **Portability:** **this is the single most valuable portable idea for an evidence-verifying CLI** — see P6.

### P3. Record rationale *side by side* with effect

**Idea.** Store the *reason* (a message, a command, a prompt, a ticket id) in the same record and the same log as the *change* it produced, so neither can drift from the other.

- **Buys:** auditability; "why" is co-located with "what" and travels with it through moves; enables a code↔rationale join without a separate system; for AI workflows it removes the "paste the answer from a private chat" problem.
- **Costs:** requires the recording layer to sit *in the loop* that produces the change (an editor or agent harness — hard for arbitrary CLI tools); doubling the data model (rationale is unbounded text); privacy exposure — every prompt and musing is now durable (heavily criticized, [S32](https://news.ycombinator.com/item?id=48492533), [S26](https://alphasignal.ai/news/zed-s-deltadb-rebuilds-version-control-around-ai-agent-conversations)); retention must be designed up front.
- **DeltaDB status:** central and claimed ("A message and the edit it produced are recorded side by side, so neither drifts away from the other," [S3](https://zed.dev/blog/introducing-deltadb)).
- **Portability:** high for anything that records *why* an assertion was made. For an evidence CLI, the "reason" is the verification command, its exit code, and the tool version — cheap and high-value.

### P4. Log + periodic checkpoints (don't choose log *or* snapshot)

**Idea.** Keep an append-only operation log for fidelity, plus periodic materialized checkpoints so opening/reading a state does not require replaying everything from the beginning.

- **Buys:** fast open (latest checkpoint + tail replay); bounded replay cost; ability to fork at any log position; checkpoints are also a natural unit for retention (you can consider pruning before a checkpoint).
- **Costs:** two code paths (write log, write checkpoint) that must agree; checkpoint invalidation when history is forked/rewritten; "timeline fork metadata" complexity; a GC policy is still required and is *not* solved by checkpoints alone.
- **DeltaDB status:** **partially primary** — the storage doc names "history packs, and checkpoints" in R2 alongside "deltas … stored in sequence" ([S16](https://delta.dev/docs/privacy-and-security/data-storage)); the release notes mention "checkpoints", "timeline fork metadata", and "avoiding increasingly expensive replay" ([S17](https://delta.dev/docs/whats-in-the-latest)). The combination is my inference.
- **Portability:** high. A CLI that writes an append-only evidence log plus a periodic compacted index is exactly this shape and needs no CRDT.

### P5. Stable file/tree identity independent of path

**Idea.** Track moves and renames as first-class events so a reference can follow a file (or a subtree) to a new path.

- **Buys:** the "moved to another file" claim in P2 actually works; renames don't invalidate history; a verification tool can say "still true, moved" instead of "missing."
- **Costs:** needs a file/tree identity model and a move-detection rule (explicit or heuristic); merge/rename interactions are genuinely hard; Git already does a form of this via content-similarity detection, and re-implementing it is expensive.
- **DeltaDB status:** implied (file-tree changes are deltas; "survives as the code moves underneath it," [S9](https://delta.dev/docs/concepts/delta-and-git), [S3](https://zed.dev/blog/introducing-deltadb)); the algorithm is unpublished.
- **Portability:** medium. If your tool already lives on Git, delegate rename detection to Git and store the *blob + old path* as evidence.

### P6. Dual verification: content fingerprint **and** re-resolvable anchor

**Idea (the direct fit for the stated example).** Do not store only a path+line range (breaks on edit) and do not store only a content hash (tells you *that* it changed, not *where it went*). Store **both**: an anchor that can be re-resolved to a current location, plus a content fingerprint of the evidence as recorded. Verification then has four outcomes: `still-true`, `moved` (anchor resolves elsewhere, fingerprint matches), `drifted` (anchor resolves, content differs → someone edited the referenced lines), `gone` (anchor resolves into deleted text or the file vanished).

- **Buys:** a truthful, actionable staleness report instead of a binary pass/fail; distinguishes "the evidence moved" from "the evidence changed" from "the evidence is gone"; keeps working across reformats and line insertions; and it can be implemented entirely in a **working tree**, with no server, no CRDT, and no multi-machine sync.
- **Costs:** you must persist the anchor log or an ID→location index (storage + a write path at capture time); you must define fuzzy-match semantics for `drifted` vs `moved` (this is the hard design decision, and different teams will want different thresholds); re-resolution needs an index or it is O(repo) per record; and *if you do not retain deleted text*, `gone` is all you can say — which is exactly the tombstone cost DeltaDB pays ([S20](https://zed.dev/blog/crdts)).
- **DeltaDB status:** the anchored half is Zed's headline claim ([S3](https://zed.dev/blog/introducing-deltadb), [S1](https://zed.dev/blog/sequoia-backs-zed)); the *verification/drift-classification* framing is mine, not DeltaDB's. DeltaDB is a versioning system, so it never needs to "verify external evidence" — this is where an independent tool can reuse the primitive and add the part DeltaDB does not do.
- **Portability:** **highest.** This is the primitive to steal.

### P7. Honest failure on divergence (never claim success you can't verify)

**Idea.** When writing into a real working tree that may have changed underneath you, preserve concurrent edits and **error out** if the state keeps moving, rather than reporting success.

- **Buys:** protects against silent corruption of a user's working tree — the highest-severity failure mode for any tool that writes files; makes conflicts visible.
- **Costs:** requires change detection (hashes/mtimes/watchers) and a re-read/retry protocol; produces user-visible errors that product owners will want to suppress; needs a defined "give up" condition.
- **DeltaDB status:** primary, and a genuinely admirable detail: "Delta preserves concurrent file edits when writing to a local checkout, and reports an error rather than claiming success if the checkout keeps changing." — 0.9.0 release notes, [S17](https://delta.dev/docs/whats-in-the-latest). Compare HN's evidence that this is a real pain point in other editors ([S33](https://news.ycombinator.com/item?id=49187256)).
- **Portability:** high, cheap, and mostly independent of everything else here.

### P8. Choose a *semantic* unit, not a keystroke

**Idea.** The unit of history should be something a producer intended, not an input event. A keystroke is not a meaningful change; a verification run, a patch, a command, or a review verdict is.

- **Buys:** less noise, smaller logs, fewer "semantically broken intermediate states," better conflict behavior, and a tractable retention policy.
- **Costs:** you lose the finest-grained anchors (your references are now coarser); you must define the unit and enforce it at capture; if you *also* want character-level anchoring you are back to storing a text CRDT, which is a large engineering commitment.
- **DeltaDB status:** Zed claims character-level granularity ([S1](https://zed.dev/blog/sequoia-backs-zed), [S2](https://zed.dev/blog/zed-1-0)) while defining a delta as an arbitrary recorded change ([S9](https://delta.dev/docs/concepts/delta-and-git)). The community critique that character granularity is the wrong unit is explicitly on record ([S22](https://github.com/zed-industries/zed/discussions/55316)); my own read is that **DeltaDB's char-level claim is about the substrate (to make anchors precise), not about the unit it presents to users.**
- **Portability:** **strong recommendation to adopt the semantic-unit version.** For an evidence CLI the unit is "one verification record," which is naturally coarse and stable.

### P9. Invertible index: "what touches this line?" and "what does this message touch?"

**Idea.** Build the index in both directions at capture time, so both queries are cheap reads rather than scans.

- **Buys:** the two headline queries in the brief ("find all conversations touching a line"; "jump from past conversation to current code") become ordinary index lookups; enables agents/tools to gather context cheaply.
- **Costs:** index maintenance on every write; index size can exceed the log; invalidation on rename/move; needs a rebuild path.
- **DeltaDB status:** claimed at feature level ([S3](https://zed.dev/blog/introducing-deltadb), [S4](https://zed.dev/deltadb)); the "navigate from code to conversation" capability is still *In Progress* on the roadmap ([S18](https://delta.dev/roadmap)).
- **Portability:** high, and the most valuable half of the traceability idea for a non-editor tool. E.g. "which recorded checks cite lines in this function?"

### P10. Virtualized worktree with real on-disk materialization

**Idea.** Version in an abstract representation, but materialize real files on disk on demand, so ordinary tools (terminals, linters, test runners) need no SDK.

- **Buys:** cheap forks at any log position; per-agent isolation without copying the repo; no special integrations required from the ecosystem.
- **Costs:** **very high.** You must keep two representations in sync, watch the filesystem, preserve EOL/gitattributes fidelity, and decide what happens when both sides change. It also substantially overlaps what `git worktree` already does well. Zed itself needed an entire new application to host it ([S5](https://zed.dev/blog/introducing-delta)).
- **DeltaDB status:** claimed and shipped in Delta ([S4](https://zed.dev/deltadb), [S10](https://delta.dev/docs/concepts/worktrees-and-machines)); the underlying representation and sync algorithm are unpublished.
- **Portability:** **low for a small tool.** Adopt only the *principle* ("work happens in real files; versioning is a transparent layer"), not the implementation. If you need cheap branches, use `git worktree` + a shadow index.

### P11. Scope-aware recording (ignore what Git ignores; never sync secrets or runtime state)

**Idea.** The recorder inherits the project's own ignore rules and explicitly excludes secrets, local config, and machine-local runtime state.

- **Buys:** avoids the "API keys in the history" failure mode; keeps log size sane; makes the tool safe to enable by default.
- **Costs:** you must implement ignore semantics correctly (nested ignore files, negations, `.gitattributes`); a user mistake now silently loses data that the tool claimed to record.
- **DeltaDB status:** primary and specific — `.gitignore` governs recording/syncing ([S9](https://delta.dev/docs/concepts/delta-and-git)); the "does not sync" table lists env vars, credentials, local services, ignored files ([S10](https://delta.dev/docs/concepts/worktrees-and-machines)). The HN "so many api keys stored in it" concern ([S32](https://news.ycombinator.com/item?id=48492533)) is exactly the risk this counters.
- **Portability:** high and cheap. For an evidence CLI, also allow an explicit allow/deny list and record *whether* a path was in scope, so "not verified" is distinguishable from "verified clean."

### P12. Access scope derived from the repository, not a parallel ACL

**Idea.** Derive who can see recorded history from repository/organization membership rather than inventing a second permission system.

- **Buys:** fewer permission surprises; history inherits the trust boundary users already understand.
- **Costs:** repository membership is often coarser than what the recorded data deserves (conversations can contain secrets and PII); the docs themselves show the subtlety — "Delta worktree history access is repository-wide, so this also grants access to every Delta worktree history for each attached repository, including histories that are not attached to the shared thread," and "Sharing a thread grants access to its attached repositories" ([S16](https://delta.dev/docs/privacy-and-security/data-storage)).
- **DeltaDB status:** primary ([S16](https://delta.dev/docs/privacy-and-security/data-storage), [S14](https://delta.dev/docs/collaboration/collaborate-thread)); roadmap has "Repository-based access" as In Progress ([S18](https://delta.dev/roadmap)).
- **Portability:** high as a *design principle*; note the documented over-grant as a cautionary tale.

---

### Recommended minimal portable stack (for the stated "evidence verifier" CLI)

If the goal is a CLI that checks whether recorded evidence is still true, the DeltaDB-derived design that pays off without importing DeltaDB's cost is:

1. **P1 + P2** — every verification record gets a stable `evidence-id`; every file reference is an anchor `(evidence-id origin, offset)` plus a content fingerprint, never a bare line range.
2. **P6** — verify by re-resolving the anchor and comparing fingerprints; classify as `still-true | moved | drifted | gone`.
3. **P8** — the recorded unit is a *semantic event* (one verification run), not a keystroke.
4. **P4** — append-only evidence log plus a periodic compacted index; do not attempt a CRDT.
5. **P7** — if you ever write to the tree, fail loudly on concurrent modification.
6. **P11 + P12** — inherit `.gitignore`, keep secrets out, and make "out of scope" a distinct outcome from "verified."

**Explicitly do not import:** the CRDT/conflict-free replicated worktree (P10 and the §4 machinery), multi-machine real-time sync, per-participant checkouts, or the "record every keystroke" granularity. Those buy collaboration at a cost — an entire synchronization engine, a server, and a retention problem — that an evidence verifier does not need.

---

## 8. Unconfirmed / not stated in any primary source

These are the things the brief asked about that **could not be confirmed**, stated explicitly rather than guessed:

1. **The delta ID scheme.** Whether a delta ID is a UUID, a `(replica_id, counter)` pair, a content hash, or something else is **not published**. Zed says "stable identity" and "unique ID" but never specifies the format. The `(replica_id, sequence_number)` + `(insertion id, offset)` scheme is documented only for **Zed's editor text CRDT** ([S20](https://zed.dev/blog/crdts)), not for DeltaDB.
2. **Whether deltas are content-addressed.** No primary source says so. Only *Git* objects are described as content-addressed and stored server-side ([S16](https://delta.dev/docs/privacy-and-security/data-storage)).
3. **Whether the anchor data structure is literally `(insertion id, offset)`.** The public-facing claim is only "anchored to a delta instead of a line number" ([S3](https://zed.dev/blog/introducing-deltadb)).
4. **What "character-level granularity" operationally means.** It is a Zed claim ([S1](https://zed.dev/blog/sequoia-backs-zed), [S2](https://zed.dev/blog/zed-1-0)) but is not reconciled with the docs' broader definition of a delta ([S9](https://delta.dev/docs/concepts/delta-and-git)). Whether every keystroke becomes a delta is unknown.
5. **Delta-level GC / compaction / pruning policy.** Not documented. Only *checkout* cleanup, local thread eviction, and whole-thread deletion are described ([S16](https://delta.dev/docs/privacy-and-security/data-storage), [S17](https://delta.dev/docs/whats-in-the-latest)).
6. **The "portals" feature.** Reported only by two secondary write-ups citing the Hanselminutes interview ([S28](https://nitinksingh.com/updates/zed-and-deltadb-version-control-between-commits/), [S29](https://jishuzhan.net/article/2096521091758601386)); no `delta.dev` doc uses the term. I could not retrieve the episode transcript ([S21](https://hanselminutes.com/1049/the-space-between-the-commits-with-zed-and-deltadbs-nathan-sobo)); the transcript is behind a PodScribe player with no fetchable text.
7. **The "event-sourced database" characterization.** Same situation: attributed to the interview via [S29](https://jishuzhan.net/article/2096521091758601386), not transcript-verified.
8. **Public protocol / API / schema / source.** No DeltaDB spec, wire format, read API, or open repo is documented. Distribution is prebuilt binaries ([S7](https://delta.dev/docs/getting-started)); a `delta cli` exists for thread deletion at minimum ([S16](https://delta.dev/docs/privacy-and-security/data-storage)) but is not documented as a third-party interface. Open-sourcing was stated as a *plan* in Aug 2025 ([S1](https://zed.dev/blog/sequoia-backs-zed)); I found no evidence it has happened.
9. **Storage, bandwidth, or latency numbers; benchmarks.** None published. Acknowledged as absent by [S30](https://sesamedisk.com/what-is-zed-deltadb-features) and [S26](https://alphasignal.ai/news/zed-s-deltadb-rebuilds-version-control-around-ai-agent-conversations).
10. **Exactly which parts shipped vs. which are roadmap.** Several anchor/trace capabilities are listed as *In Progress* ([S18](https://delta.dev/roadmap)), so announcement copy describes intent, not necessarily the current build. I could not test the product.
11. **Whether CRDT merge is applied to the code content itself or only to a "shared representation" that is then reconciled to disk.** The docs describe both a shared representation and checkouts and say "Delta keeps both synchronized" ([S9](https://delta.dev/docs/concepts/delta-and-git)) without specifying the algorithm. The release note about preserving concurrent file edits and erroring on churn ([S17](https://delta.dev/docs/whats-in-the-latest)) suggests a conservative, detect-and-fail path at least for checkout writes — but this is inference.
12. **Date of GitHub Discussion #55316.** Not shown in the fetched view (it is a community "Show and tell" post referencing the Zed 1.0 release).

---

## 9. One-paragraph summary

DeltaDB is a **log-first, CRDT-replicated, Git-complementary** layer whose central invention is making **every recorded change (and every message/comment) an addressable event with a stable identity**, so that references into code can be stored as **anchors rather than line numbers** and resolved to either "then" or "now." It stores a replayable sequence of deltas plus **checkpoints/history packs** (Cloudflare R2), keeps active-sync deltas in Durable Objects+SQLite, and materializes the resulting worktree as **real files in per-participant checkouts**, while leaving commits, branches, remotes, CI and publication in Git. Its strongest, most portable ideas are the anchor+identity model, side-by-side rationale/effect recording, and checkpointed logs; its biggest unresolved costs are **delta retention/GC**, **code-level (as opposed to data-level) merge correctness**, and the fact that the whole thing is currently reachable only through a closed-beta application with no public spec.

---

*Report compiled from public sources only. All source publication dates are listed in §0; URLs are cited inline throughout. Items that could not be confirmed from a primary source are enumerated in §8.*
