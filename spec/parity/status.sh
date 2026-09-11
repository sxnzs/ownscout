#!/usr/bin/env bash
# Compact status for every OwnScout port. Always exits 0.
#
# Lane sessions (for continuing a port with droid exec -s):
#   ts   gpt-5.6-luna      a5330512-8176-4335-9350-d28688bb3720
#   zig  gemini-3.8-flash  17338945-ff87-4c6c-947e-cfbe29955d11
#   rust glm-5.3-flash     884c8593-620b-474d-9bd5-8db093e4b082
# Superseded by the staged relaunch (the first attempt was killed by a harness
# restart before writing anything):
#   ts dace82b5-...  zig ac05b385-...  rust dc83088e-...
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
