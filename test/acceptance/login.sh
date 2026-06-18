#!/usr/bin/env bash
# Acceptance smoke for `awst login` — no real AWS calls.
# Verifies the wired-up command surface: help text, arg validation, config
# parsing errors. The happy path needs real IdC and is covered by unit tests.

set -euo pipefail

BIN="${BIN:-dist/awst}"
[[ -x "$BIN" ]] || { echo "binary not found at $BIN; run: task build"; exit 2; }

dir=$(mktemp -d)
trap 'rm -rf "$dir"' EXIT
export AWS_CONFIG_FILE="$dir/config"

fail() { echo "FAIL: $*" >&2; exit 1; }

# 1. --help works and mentions the no-browser flag
out=$("$BIN" login --help)
echo "$out" | grep -q "login <profile>" || fail "help missing usage: $out"
echo "$out" | grep -q -- "--no-browser" || fail "help missing --no-browser: $out"

# 2. Missing profile arg exits non-zero
if "$BIN" login >/dev/null 2>&1; then
  fail "login with no args should fail"
fi

# 3. Profile missing from config exits non-zero with helpful error
cat > "$AWS_CONFIG_FILE" <<'EOF'
[profile other]
region = us-east-1
EOF
if "$BIN" login dev >/dev/null 2>&1; then
  fail "login on missing profile should fail"
fi

# 4. Legacy SSO profile (no sso_session) is rejected
cat > "$AWS_CONFIG_FILE" <<'EOF'
[profile legacy]
sso_start_url = https://legacy.awsapps.com/start
sso_region = us-east-1
sso_account_id = 123456789012
sso_role_name = Developer
EOF
err=$("$BIN" login legacy 2>&1 || true)
echo "$err" | grep -q "sso_session" || fail "legacy error should mention sso_session: $err"

echo "acceptance OK"
