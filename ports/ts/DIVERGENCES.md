# TypeScript port divergences

Divergences from the Go reference that this port does not reproduce. None is
covered by `corpus.json` or `corpus-edge.json`, so both recorded corpora pass.
They are recorded here rather than silently approximated, per
`spec/parity/PORT.md`.

The two decoding gaps this file previously recorded are fixed, as is the strict
node-packet decoding path.

## 1. Lenient-path scanner error detail

On the lenient `loadPacket` path (contract validate and evidence verify),
malformed JSON that Go's `bytes.TrimSpace` strips but `encoding/json` then
rejects - leading or trailing `\v`, `\f`, NBSP, and U+0085 in particular - agrees
on the exit code (2) and on the `is not valid JSON: ` prefix, but differs in the
detail that follows it. Go emits the raw scanner text, for example
`invalid character '\v' looking for beginning of value`; this port emits its own
parser message.

Matching the detail exactly would mean porting `encoding/json`'s scanner error
strings. Not pinned by either corpus and not reached by the fuzz seeds the gate
runs.
