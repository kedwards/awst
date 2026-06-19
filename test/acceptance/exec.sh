#!/usr/bin/env bash
# Acceptance smoke for `awst exec` — no real AWS calls.

set -euo pipefail

BIN="${BIN:-dist/awst}"
[[ -x "$BIN" ]] || { echo "binary not found at $BIN; run: task build"; exit 2; }

fail() { echo "FAIL: $*" >&2; exit 1; }

# 1. --help works and mentions the key flags
out=$("$BIN" exec --help)
echo "$out" | grep -q "exec" || fail "help missing usage: $out"
echo "$out" | grep -q -- "--command" || fail "help missing --command: $out"
echo "$out" | grep -q -- "--instances" || fail "help missing --instances: $out"

# 2. Missing --command exits non-zero
if "$BIN" exec -i web >/dev/null 2>&1; then
  fail "exec without --command should fail"
fi

# 3. Missing --instances exits non-zero
if "$BIN" exec -c 'echo hi' >/dev/null 2>&1; then
  fail "exec without --instances should fail"
fi

# 4. Positional args rejected
if "$BIN" exec -c x -i y extra >/dev/null 2>&1; then
  fail "exec with positional args should fail"
fi

echo "acceptance OK"
