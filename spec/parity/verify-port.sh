#!/usr/bin/env bash
# Verify one OwnScout port against the reference corpora.
# Usage: spec/parity/verify-port.sh <name> <binary> [test-command...]
set -uo pipefail

here="$(cd "$(dirname "$0")" && pwd)"
repo="$(cd "$here/../.." && pwd)"
name="${1:?usage: verify-port.sh <name> <binary> [test-command...]}"; shift
bin="${1:?missing binary}"; shift

cd "$repo"
fail=0

if [ ! -x "$bin" ]; then
  echo "FAIL: $bin is missing or not executable"
  exit 1
fi

echo "== base corpus"
python3 spec/parity/harness.py --bin "$bin" || fail=1
echo "== edge corpus"
python3 spec/parity/harness.py --corpus spec/parity/corpus-edge.json --bin "$bin" || fail=1

if [ "$#" -gt 0 ]; then
  echo "== tests: $*"
  "$@" || fail=1
fi

echo "== protected paths"
# Ignore every port directory (lanes are disjoint); flag any change to the
# reference, the corpora, or the tooling.
touched="$(git status --porcelain | awk '{print $2}' | grep -v "^ports/" || true)"
if [ -n "$touched" ]; then
  echo "FAIL: changes outside the port directories:"
  echo "$touched"
  fail=1
fi

if [ "$fail" -eq 0 ]; then
  echo "PORT OK: $name"
fi
exit "$fail"
