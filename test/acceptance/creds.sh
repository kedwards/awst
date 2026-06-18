#!/usr/bin/env bash
# Acceptance smoke for `awst creds` — no real AWS calls.
# Verifies the contract bash users depend on (eval-able output, list format,
# clear messages).

set -euo pipefail

BIN="${BIN:-dist/awst}"
[[ -x "$BIN" ]] || { echo "binary not found at $BIN; run: task go:build"; exit 2; }

dir=$(mktemp -d)
trap 'rm -rf "$dir"' EXIT
export AWST_CREDS_DIR="$dir"

fail() { echo "FAIL: $*" >&2; exit 1; }

# 1. Empty dir
out=$("$BIN" creds list)
[[ "$out" == "No stored credentials found" ]] || fail "list (empty) got: $out"

# 2. Hand-place a creds file, list should report it
cat > "$dir/dev.env" <<'EOF'
AWS_ACCESS_KEY_ID=AKIA-FIXTURE
AWS_SECRET_ACCESS_KEY=fixture-secret
AWS_SESSION_TOKEN=fixture-token==
EOF
out=$("$BIN" creds list)
echo "$out" | grep -q "dev" || fail "list did not include dev: $out"
echo "$out" | grep -q "ago" || fail "list missing age string: $out"

# 3. use prints eval-able exports preserving '=' in token
out=$("$BIN" creds use dev)
echo "$out" | grep -qx 'export AWS_ACCESS_KEY_ID="AKIA-FIXTURE"' || fail "use AKI line: $out"
echo "$out" | grep -qx 'export AWS_SESSION_TOKEN="fixture-token=="' || fail "use token line: $out"
echo "$out" | grep -qx 'export AWS_PROFILE="dev"' || fail "use profile line: $out"

# 4. clear named profile
"$BIN" creds clear dev | grep -q "dev" || fail "clear message missing profile"
[[ ! -f "$dir/dev.env" ]] || fail "clear did not delete file"

# 5. clear unknown profile exits non-zero
if "$BIN" creds clear ghost 2>/dev/null; then
  fail "clear of unknown profile should fail"
fi

echo "acceptance OK"
