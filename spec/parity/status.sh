#!/usr/bin/env bash
# Compact status for every OwnScout port. Always exits 0.
#
# Lane sessions (for continuing a port with droid exec -s):
#   ts   gpt-5.6-luna      dace82b5-dedc-4fce-a975-e20685b280a6
#   zig  gemini-3.8-flash  ac05b385-a3e9-4e76-9bf0-f4c25d217e54
#   rust glm-5.3-flash     dc83088e-3578-468c-af21-4e15392ed837
set -uo pipefail

here="$(cd "$(dirname "$0")" && pwd)"
repo="$(cd "$here/../.." && pwd)"
cd "$repo"

printf '%-5s %-9s %-14s %-14s %s\n' PORT BINARY BASE EDGE PATH
for entry in "ts:ports/ts/bin/ownscout" "zig:ports/zig/zig-out/bin/ownscout" "rust:ports/rust/target/release/ownscout"; do
  name="${entry%%:*}"; bin="${entry#*:}"
  if [ ! -x "$bin" ]; then
    printf '%-5s %-9s %-14s %-14s %s\n' "$name" missing - - "$bin"
    continue
  fi
  base="$(python3 spec/parity/harness.py --bin "$bin" 2>&1 | tail -1)"
  edge="$(python3 spec/parity/harness.py --corpus spec/parity/corpus-edge.json --bin "$bin" 2>&1 | tail -1)"
  printf '%-5s %-9s %-14s %-14s %s\n' "$name" present "${base#harness: }" "${edge#harness: }" "$bin"
done
exit 0
