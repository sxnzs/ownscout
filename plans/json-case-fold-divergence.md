# Case-folded JSON field names: the recorded two-decoder divergence

Independently verified from the reference binary (`spec/parity/reference-ownscout`,
commit `ede2d25`). This pins the exact contract the ports must reproduce for the
case-insensitive-field-matching divergence (fuzz seed 3, iteration 48).

## The asymmetry that matters

The reference uses **two different packet decoders**, and they disagree on case:

| Command | Decoder | Field matching |
|---|---|---|
| `contract validate` | `internal/cli/adapter.go` `loadPacket` → `json.Decoder` + `DisallowUnknownFields()` | **ASCII case-fold fallback** (Go `encoding/json`) |
| `evidence verify` | same `loadPacket` | **ASCII case-fold fallback** |
| `node verify` | `internal/nodepacket/decode.go` `checkValue` → `exactField` | **exact only, no fold** |

So `{"PACKET_ID": ...}` is **accepted by `contract validate`/`evidence verify` and
rejected by `node verify`** in the same reference build. Ports that share one
strict decoder across all three commands get one of the two halves wrong.

Verified against the reference:

```
contract validate {"PACKET_ID":...}   -> exit 0  "packet is valid"
evidence  verify   {"PACKET_ID":...}  -> exit 0  "evidence verified"
node      verify   {"PACKET_ID":...}  -> exit 2  "packet could not be decoded / strict packet decoding failed"
```

## Exact fold semantics (validate/evidence path)

All verified against `reference-ownscout contract validate`:

- **ASCII-only.** `öutcome` (non-ASCII) is still `unknown field "öutcome"`. Go's
  fold is `foldName`, ASCII case folding — it does not Unicode-fold.
- **Fold applies to the value's type check, not just key acceptance.**
  `{"BUDGET": "x"}` yields `cannot unmarshal string into ... Packet.budget ...`,
  not `unknown field`. A folded key routes to the field's typed decoder.
- **Recursive.** Folds inside nested objects (`freshness.STATUS`,
  `budget.MAX_EVIDENCE`) and array elements (`evidence[0].KIND`,
  `evidence[0].COMMIT`).
- **Last-wins on duplicates.** `{"OUTCOME":"blocked","outcome":"complete"}` and
  the reverse both decode; document order decides the surviving value.
- **Document-order error precedence holds.** An unknown key appearing *before* a
  foldable key is still reported as unknown (`{"zzz_first":1,"PACKET_ID":"x"}`
  → `unknown field "zzz_first"`). Folding does not change which error is first.

## Fuzz evidence

`python3 spec/parity/fuzz.py --seed 3 --iterations 60` diverges at iteration 48:
Go case-folds `schEma_version` and reports the *later* genuinely-unknown
`us\uFFFDd_bytes`; the ports reject `schEma_version` outright. Confirmed live.

## Per-port work

- **node verify path:** already correct in all three (TS confirmed byte-exact on
  the rejection). Do not fold there.
- **contract/evidence path:** add ASCII case-fold fallback to the field resolver,
  routed through the field's typed check, preserving document-order precedence.

Related open finding (not covered here): invalid/truncated UTF-8 emits a
different U+FFFD count — Go (`utf8.DecodeRune`, per-byte) emits more than TS
(`TextDecoder`, maximal-subpart) and Rust; Zig matches Go. Separate fix.
