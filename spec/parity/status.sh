#!/usr/bin/env bash
# Compact status for every OwnScout port. Always exits 0.
#
# Lane sessions (for continuing a port with droid exec -s):
#   ts   gpt-5.6-luna  a5330512-8176-4335-9350-d28688bb3720  (done: 28/28, 81/81)
#   zig  gpt-5.6-luna  7cbb340e-8fae-4c5c-8e50-b934b51e2ec8
#   rust gpt-5.6-luna  9b93b98f-67ee-4c10-a062-3fbc9e0cb03b
# Superseded: first attempt (harness restart, no writes) ts dace82b5-... zig
# ac05b385-... rust dc83088e-...; second attempt (over-analysis, stub only) zig
# 17338945-... rust 884c8593-...
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
