#!/usr/bin/env bash
# Acceptance smoke for `awst exec` — no real AWS calls.

set -euo pipefail

BIN="${BIN:-dist/awst}"
[[ -x "$BIN" ]] || { echo "binary not found at $BIN; run: task build"; exit 2; }

fail() { echo "FAIL: $*" >&2; exit 1; }

# 1. --help works and carries the surface `exec` shares with `run`
out=$("$BIN" exec --help)
echo "$out" | grep -q "exec" || fail "help missing usage: $out"
for flag in --command --file --dir --list --profile --region --instances; do
  echo "$out" | grep -q -- "$flag" || fail "help missing $flag: $out"
done

# 2. Missing --command exits non-zero
if "$BIN" exec -i web >/dev/null 2>&1; then
  fail "exec without --command should fail"
fi

# 3. Missing --instances exits non-zero
if "$BIN" exec -c 'echo hi' >/dev/null 2>&1; then
  fail "exec without --instances should fail"
fi

# 4. A saved command name alongside -c is rejected
if "$BIN" exec -c x -i y extra >/dev/null 2>&1; then
  fail "exec with both -c and a saved command name should fail"
fi

# 5. -l lists saved commands from a fixture dir
dir=$(mktemp -d)
trap 'rm -rf "$dir"' EXIT
cat > "$dir/rate-check" <<'EOF'
# description: vendor connectivity check
curl -sS https://example.invalid
EOF

out=$("$BIN" exec -d "$dir" -l)
echo "$out" | grep -q "rate-check" || fail "list missing rate-check: $out"
echo "$out" | grep -q "vendor connectivity check" || fail "list missing description: $out"

# 6. No command source in a pipe lists and exits non-zero
if out=$("$BIN" exec -d "$dir" 2>/dev/null); then
  fail "no command source should fail in a pipe"
fi
echo "$out" | grep -q "rate-check" || fail "no-source error should still print the list: $out"

# 7. -d to a nonexistent dir errors
if "$BIN" exec -d /no/such/dir rate-check >/dev/null 2>&1; then
  fail "missing -d directory should fail"
fi

echo "acceptance OK"
