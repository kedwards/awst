#!/usr/bin/env bash
# Acceptance smoke for `awst connect` — no real AWS calls.
# Verifies help + arg validation + the plugin-missing guard.

set -euo pipefail

BIN="${BIN:-dist/awst}"
[[ -x "$BIN" ]] || { echo "binary not found at $BIN; run: task build"; exit 2; }

fail() { echo "FAIL: $*" >&2; exit 1; }

# 1. --help works and mentions the key surface
out=$("$BIN" connect --help)
echo "$out" | grep -q "connect \[instance\]" || fail "help missing usage: $out"
echo "$out" | grep -q -- "--profile" || fail "help missing --profile: $out"
echo "$out" | grep -q -- "--region" || fail "help missing --region: $out"
echo "$out" | grep -q "session-manager-plugin" || fail "help should mention plugin dep: $out"

# 2. Plugin guard — point AWST_SSM_PLUGIN at something that definitely doesn't exist.
err=$(AWST_SSM_PLUGIN=/nonexistent/plugin-binary "$BIN" connect web 2>&1 || true)
echo "$err" | grep -q "session-manager-plugin" || fail "plugin-missing error should name the binary: $err"

# 3. Too many positional args rejected
if "$BIN" connect a b >/dev/null 2>&1; then
  fail "connect with two args should fail"
fi

echo "acceptance OK"
