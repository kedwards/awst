#!/usr/bin/env bash
# Acceptance smoke for `awst list` + `awst kill` — no real AWS / no real
# process killing. Verifies the wired command surface only.

set -euo pipefail

BIN="${BIN:-dist/awst}"
[[ -x "$BIN" ]] || { echo "binary not found at $BIN; run: task build"; exit 2; }

fail() { echo "FAIL: $*" >&2; exit 1; }

# 1. list --help works
out=$("$BIN" list --help)
echo "$out" | grep -q "list" || fail "list help missing usage: $out"

# 2. kill --help works and mentions --all
out=$("$BIN" kill --help)
echo "$out" | grep -q "kill" || fail "kill help missing usage: $out"
echo "$out" | grep -q -- "--all" || fail "kill help missing --all: $out"

# 3. kill without args / flag exits non-zero
if "$BIN" kill >/dev/null 2>&1; then
  fail "kill with no args should fail"
fi

# 4. kill rejects non-numeric pid
if "$BIN" kill bogus >/dev/null 2>&1; then
  fail "kill with non-numeric pid should fail"
fi

# 5. kill rejects --all combined with PIDs
if "$BIN" kill --all 123 >/dev/null 2>&1; then
  fail "kill --all combined with PIDs should fail"
fi

# 6. kill on a pid that doesn't exist returns non-zero (SIGTERM ESRCH)
if "$BIN" kill 99999999 >/dev/null 2>&1; then
  fail "kill on nonexistent pid should fail"
fi

# 7. list when no sessions active prints the no-sessions message (assumes
#    this test host isn't running any awst-started SSM sessions).
out=$("$BIN" list)
echo "$out" | grep -qE "no active|PID" || fail "list output unexpected: $out"

echo "acceptance OK"
