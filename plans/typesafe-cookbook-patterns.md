# TypeSafe cookbook patterns — distilled 2026-09-18

Scope: all 18 cookbook pages listed by `https://docs.typesafe.ai/llms.txt`, plus the two demo pages. “Not reported” means the cookbook did not publish that measurement; it is not an implied zero. Prices are the pages’ historical examples, not a recommendation.

## Cookbooks

### Self-consistency: nouls
| field | content |
|---|---|
| name + URL | Self-consistency: nouls — [page](https://docs.typesafe.ai/cookbooks/consistency_noul_cookbook.md) |
| problem it solves | Detect unstable yes/no claim-triage judgments and abstain near a policy boundary. |
| the pattern | Code repeats one 14-question rubric 15 times and retains every probability; TypeSafe judges each Noul, while code computes variation and maps `0.30–0.70` to human review. |
| primitives used | 14 Nouls; 15 samples/condition; Jev mean per-question SD `0.0102`; covered answers `0.43–0.53`; uncertainty bounds `0.30`, `0.70`; historical price `$0.042/1M` input, `$0` output. |
| OwnScout lesson | A future companion can expose uncertainty, but deterministic ledger admission and evidence verdicts stay in Go; store raw judgments and thresholds as auditable events. |

### Self-consistency: choices
| field | content |
|---|---|
| name + URL | Self-consistency: choices — [page](https://docs.typesafe.ai/cookbooks/consistency_choice_cookbook.md) |
| problem it solves | Prevent a borderline moderation label from randomly changing the route. |
| the pattern | Repeat an 8-Choice rubric 15 times; code compares label agreement and makes low top-probability answers `uncertain`; TypeSafe only selects labels and supplies probabilities. |
| primitives used | 8 Choices; `15` repeats; Jev plurality repetition `90.8%`; LLM comparison `87.5–100%`; top-probability gate `0.60`; gated Jev agreement `99.2%`, automatic answers `74.2%`; historical `$0.042/1M` input. |
| OwnScout lesson | Route uncertain evidence to a review artifact rather than silently changing a deterministic verdict. |

### Parallel questions
| field | content |
|---|---|
| name + URL | Parallel questions — [page](https://docs.typesafe.ai/cookbooks/parallel_questions.md) |
| problem it solves | Avoid paying and waiting for the same large document once per question. |
| the pattern | Put all independent questions in one `system_one` request; code keeps each answer separate and compares batched vs one-at-a-time repeats. |
| primitives used | 13 questions: 8 Nouls, 2 Choices, 3 Scores; `5` repeats; GDPR text `53,777` characters; `12.2x` cheaper and `10.0x` faster, with no answer change; historical `$0.042/1M` input. |
| OwnScout lesson | Batch independent companion checks only in an optional network process; the core replay remains offline and deterministic. |

### Re-ranking
| field | content |
|---|---|
| name + URL | Re-ranking — [page](https://docs.typesafe.ai/cookbooks/rerank_typesafe.md) |
| problem it solves | Improve a fast lexical shortlist without comparing every corpus item semantically. |
| the pattern | BM25 deterministically selects `30` candidates/query from `3,565` passages; one Noul scores each query-candidate pair; code sorts descending. |
| primitives used | Noul; `40` queries; top-1 `5%→18%`; top-10 `38%→62%`; no fixed confidence gate reported. |
| OwnScout lesson | Retrieve deterministically, then optionally rank evidence candidates; never let a ranker replace source hashes or provenance. |

### Line-by-line search
| field | content |
|---|---|
| name + URL | Line-by-line search — [page](https://docs.typesafe.ai/cookbooks/semantic_find.md) |
| problem it solves | Find quotable answer lines while distinguishing “closest line” from “document answers it.” |
| the pattern | Code tags lines; a Choice ranks IDs and a Noul tests existence in the same call; code uses `exists ≥0.70` = answered, `<0.35` = absent, otherwise partial. |
| primitives used | Choice up to `255` options (218 lines in example) + Noul; thresholds `0.70`, `0.35`; observed typical exists `≥0.90` when present and `≤0.05` when absent. |
| OwnScout lesson | Keep exact line/byte offsets and hash them locally; use a future Noul only as a review signal, never as proof. |

### Structure recovery
| field | content |
|---|---|
| name + URL | Structure recovery — [page](https://docs.typesafe.ai/cookbooks/autoformat.md) |
| problem it solves | Restore Markdown structure without rewriting source characters. |
| the pattern | Pass 1 asks whether adjacent lines continue a sentence; code merges. Pass 2 classifies blocks and asks companion questions; code renders from original text. Explicit markers/blank lines remain deterministic. |
| primitives used | Pass 1 Nouls; pass 2 Choices plus conditional heading/step/callout Choices; `2` requests; `10,211` tokens; `0.8s`; `$0.0015` for the memo. |
| OwnScout lesson | Ask only about ambiguity; render the final evidence record from bytes owned by code. |

### Function calling
| field | content |
|---|---|
| name + URL | Function calling — [page](https://docs.typesafe.ai/cookbooks/function_calling.md) |
| problem it solves | Map natural language to safe calls with typed, closed-set arguments. |
| the pattern | Code introspects signatures; Choices select `Literal` values, Nouls say whether optional arguments were stated, and code dispatches ordinary functions. Free text, numbers, dates, and defaults stay in code. |
| primitives used | Choice for closed-set values, Noul for “stated?”; `10` functions, `28` fillable arguments; no routing threshold reported. |
| OwnScout lesson | A Jev companion may choose among explicit evidence commands, but the Go dispatcher validates every argument and owns side effects. |

### Skill suggestion
| field | content |
|---|---|
| name + URL | Skill suggestion — [page](https://docs.typesafe.ai/cookbooks/skill_suggestion.md) |
| problem it solves | Avoid loading the wrong skill or loading one when no skill applies. |
| the pattern | Call 1 ranks all skills and gates need; code takes top `3`. Call 2 reranks those with fuller text and can reject all; code emits at most one name. |
| primitives used | Choice over `182` skills; Nouls for need and fit; shortlist `3`; gates `0.30` (mean need and best-fit); `488` requests. Wrong loads `16.8%→7.3%`; needless `9.8%→4.0%`. |
| OwnScout lesson | Bound candidate fan-out and make “none applies” explicit; do not make network judgment a prerequisite for basic verification. |

### Knowledge graph entity alignment
| field | content |
|---|---|
| name + URL | Knowledge graph entity alignment — [page](https://docs.typesafe.ai/cookbooks/entity_alignment.md) |
| problem it solves | Decide whether candidate entities are different, curator-worthy, or safe to merge. |
| the pattern | One ordered Score encodes the three actions; code rounds to a level and routes. Three field Nouls explain disagreement only for curator cases. No fitted numeric threshold. |
| primitives used | Score with `3` levels + `3` Nouls; `450` candidate pairs; no threshold; endpoint pool `6` workers (rate-limits above roughly `8`). |
| OwnScout lesson | Prefer explicit “review” as a third outcome over forcing a binary merge; map that to a review ledger event. |

### Classifying RAG passages
| field | content |
|---|---|
| name + URL | Classifying RAG passages — [page](https://docs.typesafe.ai/cookbooks/classifying_rag_passages.md) |
| problem it solves | Keep irrelevant, contradictory, and prompt-injecting retrieval results away from generation. |
| the pattern | Embeddings retrieve top `12` from `81`; four Nouls score each passage; code applies first-match thresholds and separates evidence/conflict. |
| primitives used | 4 Nouls; `TOP_K=12`; injection `>0.70`, contradiction `>0.70`, relevance `<0.45`, evidence `>0.55`; `256` embedding dims; `80` copied + `1` planted passage. |
| OwnScout lesson | Make route order and thresholds named, reviewable code; retain excluded/conflicting evidence rather than deleting audit history. |

### Double-checking citations
| field | content |
|---|---|
| name + URL | Double-checking citations — [page](https://docs.typesafe.ai/cookbooks/citation_check.md) |
| problem it solves | Catch fabricated, unsupported, or contradicted citations. |
| the pattern | Code first performs exact quote matching; TypeSafe Choice classifies surviving context; code auto-accepts only high confidence and sends the rest to a human. |
| primitives used | Choice with `supports/contradicts/says nothing`; `8` citations, `4` accurate and `4` planted failures; confidence gate `0.80`; accurate examples `≥0.93`. |
| OwnScout lesson | Exact local matching and source hashes precede any semantic companion; confidence can triage, never authenticate. |

### Guardrails for LLMs
| field | content |
|---|---|
| name + URL | Guardrails for LLMs — [page](https://docs.typesafe.ai/cookbooks/llm_guardrails.md) |
| problem it solves | Screen inputs and outputs for hazards with policy thresholds outside the generator. |
| the pattern | One request carries hazard Nouls and a harm Score; code routes pass/review/block/support. Run the same battery before and after the LLM. |
| primitives used | `4` Nouls + `1` Score; Score has `4` levels (none, mild, serious, severe); exact route thresholds are policy constants in code, not published as one universal number. |
| OwnScout lesson | Keep safety policy explicit and versioned; a companion can flag, but cannot alter the hash chain. |

### SDE cascade
| field | content |
|---|---|
| name + URL | SDE cascade — [page](https://docs.typesafe.ai/cookbooks/sde_cascade.md) |
| problem it solves | Get much of a reasoning model’s extraction quality at lower cost. |
| the pattern | Cheap model extracts; TypeSafe Nouls verify per-field error signals; code escalates to reasoning only when any signal exceeds the gate. |
| primitives used | Per-field Nouls plus holistic head; `FIRE_T=0.70`; mini `$0.75/$4.50` per 1M, reasoning `$5/$30`, Jev `$0.042/$0`; 100-prompt cost/quality sweep. |
| OwnScout lesson | A future verifier may propose escalation, but only deterministic schema/normalization and explicit gates may commit extracted evidence. |

### Date extraction
| field | content |
|---|---|
| name + URL | Date extraction — [page](https://docs.typesafe.ai/cookbooks/date_extraction_cookbook.md) |
| problem it solves | Read date parts from prose while keeping calendar arithmetic and validation deterministic. |
| the pattern | Seven Choices read mode and parts; code resolves absolute/relative dates against fixed `TODAY`, validates, and reviews low confidence. |
| primitives used | `7` Choices; confidence gate `0.60`; year options `1900..2050` plus `none` and `out_of_range`; fixed today `2026-07-30`. |
| OwnScout lesson | Let a companion extract candidate fields only; Go owns parsing, timezone/date rules, and rejection. |

### Pre-parsed value extraction
| field | content |
|---|---|
| name + URL | Pre-parsed value extraction — [page](https://docs.typesafe.ai/cookbooks/pre_parsed_value_extraction_cookbook.md) |
| problem it solves | Select a requested email/phone/amount without allowing invented or altered values. |
| the pattern | Regex over-finds candidates; Choice selects one candidate or `none`; code copies verbatim and normalizes, while Nouls classify attributes such as credit. |
| primitives used | Choices over candidate spans + Noul; no numeric gate reported; regexes for email, phone, money; `none` escape on every selection. |
| OwnScout lesson | This is the safest companion boundary: candidate bytes and normalization remain deterministic and auditable. |

### Hierarchical classification
| field | content |
|---|---|
| name + URL | Hierarchical classification — [page](https://docs.typesafe.ai/cookbooks/hierarchical_classification.md) |
| problem it solves | Classify into deep taxonomies without losing observability or exploring every path. |
| the pattern | Choice probabilities are tree edges. Greedy keeps the best child; beam keeps `K` paths, prunes by geometric-mean edge probability, and code returns the best leaf. |
| primitives used | Choice; beam width `K` is configurable (exact demo value not stated in prose); path score `product(probabilities) ** (1/decisions)`; separation is top/second ratio; four hierarchies. |
| OwnScout lesson | Use bounded candidate paths for optional evidence categorization; preserve every selected path and taxonomy version. |

### Autoresearch feature discovery
| field | content |
|---|---|
| name + URL | Autoresearch feature discovery — [page](https://docs.typesafe.ai/cookbooks/autoresearch_feature_discovery.md) |
| problem it solves | Turn free text into useful numeric features and iteratively improve a supervised model. |
| the pattern | An LLM proposes questions; TypeSafe answers them; CatBoost reports errors/feature use; the next proposal revises the question set. Code owns folds, training, and metrics. |
| primitives used | `29` Score questions × 2 columns + `9` Nouls = `67` numeric columns; `2,000` reviews, `800` held out; `5` folds × `3` repeats; RMSE `3.09→2.47→2.15→1.87→1.77`; `18` proposals/round. |
| OwnScout lesson | Treat question design as an offline experiment; never mutate production verification policy from an unreviewed loop. |

### Classification using confidence
| field | content |
|---|---|
| name + URL | Classification using confidence — [page](https://docs.typesafe.ai/cookbooks/classification_using_confidence.md) |
| problem it solves | Report a broad, safer label when a fine-grained classification is uncertain. |
| the pattern | One Choice selects among `75` SIC groups; code checks confidence and rolls up to the known division without another call. |
| primitives used | Choice; gate `0.90`; `60` filings; confident half `90%` correct vs other half `40%`; rollup raises the latter to `70%`; `444` four-digit codes, `10` divisions. |
| OwnScout lesson | Confidence may select review granularity, not factual truth; a broad evidence status must still cite the same bytes. |

## Demo pages

### Demos index
| field | content |
|---|---|
| name + URL | Demos — [page](https://docs.typesafe.ai/demos.md) |
| problem it solves | Points to interactive examples. |
| the pattern | The index names Smart Home Assistant and its speculative fan-out pattern; no measurements are supplied. |
| primitives used | Not specified on the index. |
| OwnScout lesson | Treat demos as patterns to inspect, not production guarantees. |

### Smart Home Assistant demo
| field | content |
|---|---|
| name + URL | Smart Home Assistant — [page](https://docs.typesafe.ai/demos/smart-home.md) |
| problem it solves | Route smart-home commands, compound actions, and general conversation. |
| the pattern | Speculatively ask category/domain/device/action in one call; code discards irrelevant answers. A Noul detects compound requests for an LLM splitter; an LLM handles freeform conversation. |
| primitives used | Speculative fan-out; one example has 4 relevant questions; no thresholds, probabilities, cost, or latency numbers published (only “negligible” added latency versus the LLM). |
| OwnScout lesson | Fan-out belongs in an optional companion; code filters and executes only validated, deterministic commands. |

## Synthesis

### Five repeated patterns
1. **Code owns the final action:** TypeSafe supplies a narrow typed judgment; thresholds, ordering, normalization, rendering, and side effects stay deterministic and reviewable.
2. **Fan out independent questions:** Batch a battery or speculative questions so shared state is paid for once; filter irrelevant answers afterward.
3. **Use a closed output space:** Choice/Score levels, candidate spans, line IDs, and explicit `none`/review outcomes prevent free-form invention.
4. **Make uncertainty operational:** Confidence/probability gates choose abstain, broader labels, curator review, or escalation; they are per-policy constants, not universal truths.
5. **Cascade and preserve evidence:** Cheap retrieval/extraction comes first, TypeSafe verifies, and an expensive model or human sees only flagged cases; raw scores, source context, and routes remain inspectable.

### Glossary (the docs’ vocabulary)
- **State:** the structured context supplied to a System One request; questions evaluate the same state independently.
- **Question / primitive:** a typed request for a judgment: **Noul** (P(true)), **Choice** (one option plus distribution), or **Score** (ordered level plus distribution).
- **Judgment:** the typed answer to a narrow question, not generated prose.
- **Probability / noul:** probability assigned to an outcome; a Noul specifically is the probability that its yes/true criterion holds.
- **Confidence:** TypeSafe’s certainty/concentration signal for an answer; distinct from the probability of the selected proposition.
- **Calibration:** probabilities intended to behave like empirical frequencies; use held-out examples and policy gates rather than assuming a cutoff transfers.
- **Escalation:** code sends a flagged result to a stronger model, curator, or human review.
- **Cascade:** staged cheap extraction → TypeSafe verification → expensive reasoning only when a verifier signal fires.
- **Fan-out:** many independent (including speculative) questions in one request, with code deciding which answers matter.
- **System One:** TypeSafe’s model/API style: fast structured decisions that code can consume directly.

## Compatibility notes

The corpus confirms, rather than contradicts, the key skill guidance: questions are ordinary visible dictionary keys in the examples; thresholds are application/policy constants and vary by primitive and task; confidence is not probability; and code remains in control. The docs add a useful nuance: a Choice can still pick a “best” option when none fits, so pair it with a Noul or explicit `none` option. They also show Score as a threshold-free ordered route when the middle review state is itself a semantic level.
