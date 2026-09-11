#!/usr/bin/env bash
# Grade every OwnScout port. Run from anywhere.
set -uo pipefail

here="$(cd "$(dirname "$0")" && pwd)"
repo="$(cd "$here/../.." && pwd)"
cd "$repo"

rc=0
grade() {
  local name="$1" binary="$2" testcmd="$3"
  if [ ! -e "$binary" ]; then
    echo "SKIP: $name (no binary at $binary)"
    rc=1
    return
  fi
  if [ -n "$testcmd" ]; then
    "$here/verify-port.sh" "$name" "$binary" bash -c "$testcmd" || rc=1
  else
    "$here/verify-port.sh" "$name" "$binary" || rc=1
  fi
}

grade ts   ports/ts/bin/ownscout          "cd ports/ts && node --test"
grade zig  ports/zig/zig-out/bin/ownscout "cd ports/zig && zig build test"
grade rust ports/rust/target/release/ownscout "cd ports/rust && cargo test --quiet"

if [ "$rc" -eq 0 ]; then
  echo "ALL PORTS OK"
else
  echo "SOME PORTS FAILED OR ARE MISSING"
fi
exit "$rc"
